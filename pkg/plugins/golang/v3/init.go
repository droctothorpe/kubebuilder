/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v3

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/pflag"

	"sigs.k8s.io/kubebuilder/v2/pkg/internal/validation"
	"sigs.k8s.io/kubebuilder/v2/pkg/model/config"
	"sigs.k8s.io/kubebuilder/v2/pkg/plugin"
	"sigs.k8s.io/kubebuilder/v2/pkg/plugins/golang/v3/scaffolds"
	"sigs.k8s.io/kubebuilder/v2/pkg/plugins/internal/cmdutil"
	"sigs.k8s.io/kubebuilder/v2/pkg/plugins/internal/util"
)

type initSubcommand struct {
	config *config.Config
	// For help text.
	commandName string

	// boilerplate options
	license string
	owner   string

	// flags
	fetchDeps          bool
	skipGoVersionCheck bool
}

var (
	_ plugin.InitSubcommand = &initSubcommand{}
	_ cmdutil.RunOptions    = &initSubcommand{}
)

func (p *initSubcommand) UpdateContext(ctx *plugin.Context) {
	ctx.Description = `Initialize a new project including vendor/ directory and Go package directories.

Writes the following files:
- a boilerplate license file
- a PROJECT file with the domain and repo
- a Makefile to build the project
- a go.mod with project dependencies
- a Kustomization.yaml for customizating manifests
- a Patch file for customizing image for manager manifests
- a Patch file for enabling prometheus metrics
- a main.go to run
`
	ctx.Examples = fmt.Sprintf(`  # Scaffold a project using the apache2 license with "The Kubernetes authors" as owners
  %s init --project-version=2 --domain example.org --license apache2 --owner "The Kubernetes authors"
`,
		ctx.CommandName)

	p.commandName = ctx.CommandName
}

func (p *initSubcommand) BindFlags(fs *pflag.FlagSet) {
	fs.BoolVar(&p.skipGoVersionCheck, "skip-go-version-check",
		false, "if specified, skip checking the Go version")

	// dependency args
	fs.BoolVar(&p.fetchDeps, "fetch-deps", true, "ensure dependencies are downloaded")

	// boilerplate args
	fs.StringVar(&p.license, "license", "apache2",
		"license to use to boilerplate, may be one of 'apache2', 'none'")
	fs.StringVar(&p.owner, "owner", "", "owner to add to the copyright")
	fs.BoolVar(&p.config.ComponentConfig, "component-config", false,
		"create a versioned ComponentConfig file, may be 'true' or 'false'")

	// project args
	fs.StringVar(&p.config.Repo, "repo", "", "name to use for go module (e.g., github.com/user/repo), "+
		"defaults to the go package of the current working directory.")
	fs.StringVar(&p.config.Domain, "domain", "my.domain", "domain for groups")
	fs.StringVar(&p.config.ProjectName, "project-name", "", "name of this project")
}

func (p *initSubcommand) InjectConfig(c *config.Config) {
	c.Layout = plugin.KeyFor(Plugin{})
	p.config = c
}

func (p *initSubcommand) Run() error {
	return cmdutil.Run(p)
}

func (p *initSubcommand) Validate() error {
	// Requires go1.11+
	if !p.skipGoVersionCheck {
		if err := util.ValidateGoVersion(); err != nil {
			return err
		}
	}

	// Check if the current directory has not files or directories which does not allow to init the project
	if err := checkDir(); err != nil {
		return err
	}

	// Check if the project name is a valid k8s namespace (DNS 1123 label).
	if p.config.ProjectName == "" {
		dir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("error getting current directory: %v", err)
		}
		p.config.ProjectName = strings.ToLower(filepath.Base(dir))
	}
	if err := validation.IsDNS1123Label(p.config.ProjectName); err != nil {
		return fmt.Errorf("project name (%s) is invalid: %v", p.config.ProjectName, err)
	}

	// Try to guess repository if flag is not set.
	if p.config.Repo == "" {
		repoPath, err := util.FindCurrentRepo()
		if err != nil {
			return fmt.Errorf("error finding current repository: %v", err)
		}
		p.config.Repo = repoPath
	}

	return nil
}

func (p *initSubcommand) GetScaffolder() (cmdutil.Scaffolder, error) {
	return scaffolds.NewInitScaffolder(p.config, p.license, p.owner), nil
}

func (p *initSubcommand) PostScaffold() error {
	if !p.fetchDeps {
		fmt.Println("Skipping fetching dependencies.")
		return nil
	}

	// Ensure that we are pinning controller-runtime version
	// xref: https://github.com/kubernetes-sigs/kubebuilder/issues/997
	err := util.RunCmd("Get controller runtime", "go", "get",
		"sigs.k8s.io/controller-runtime@"+scaffolds.ControllerRuntimeVersion)
	if err != nil {
		return err
	}

	err = util.RunCmd("Update go.mod", "go", "mod", "tidy")
	if err != nil {
		return err
	}

	// TODO: make this conditional with a '--make' flag, like in 'create api'.
	err = util.RunCmd("Running make", "make")
	if err != nil {
		return err
	}

	fmt.Printf("Next: define a resource with:\n$ %s create api\n", p.commandName)
	return nil
}

// checkDir will return error if the current directory has files which are
// not the go.mod and/or starts with the prefix (.) such as .gitignore.
// Note that, it is expected that the directory to scaffold the project is cleaned.
// Otherwise, it might face issues to do the scaffold. The go.mod is allowed because user might run
// go mod init before use the plugin it for not be required inform
// the go module via the repo --flag.
func checkDir() error {
	err := filepath.Walk(".",
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.Name() != "go.mod" && !strings.HasPrefix(info.Name(), ".") {
				return errors.New("only the go.mod and files with the prefix \"(.)\" are allowed before the init")
			}
			return nil
		})
	if err != nil {
		return err
	}
	return nil
}
