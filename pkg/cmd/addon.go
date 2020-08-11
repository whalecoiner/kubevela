package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/ghodss/yaml"

	"github.com/crossplane/oam-kubernetes-runtime/apis/core/v1alpha2"

	"github.com/gosuri/uitable"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloud-native-application/rudrx/pkg/utils/system"

	"github.com/cloud-native-application/rudrx/pkg/plugins"

	"github.com/cloud-native-application/rudrx/api/types"

	cmdutil "github.com/cloud-native-application/rudrx/pkg/cmd/util"
	"github.com/spf13/cobra"
)

func AddonCommandGroup(parentCmd *cobra.Command, c types.Args, ioStream cmdutil.IOStreams) {
	parentCmd.AddCommand(
		NewAddonConfigCommand(ioStream),
		NewAddonListCommand(ioStream),
		NewAddonUpdateCommand(ioStream),
		NewAddonInstallCommand(c, ioStream),
	)
}

func NewAddonConfigCommand(ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "addon:config <reponame> <url>",
		Short:   "Set the addon center, default is local (built-in ones)",
		Long:    "Set the addon center, default is local (built-in ones)",
		Example: `vela addon:config myhub https://github.com/oam-dev/catalog/repository`,
		RunE: func(cmd *cobra.Command, args []string) error {
			argsLength := len(args)
			if argsLength < 2 {
				return errors.New("please set addon repo with <RepoName> and <URL>")
			}
			repos, err := plugins.LoadRepos()
			if err != nil {
				return err
			}
			config := &plugins.RepoConfig{
				Name:    args[0],
				Address: args[1],
				Token:   cmd.Flag("token").Value.String(),
			}
			var updated bool
			for idx, r := range repos {
				if r.Name == config.Name {
					repos[idx] = *config
					updated = true
					break
				}
			}
			if !updated {
				repos = append(repos, *config)
			}
			if err = plugins.StoreRepos(repos); err != nil {
				return err
			}
			ioStreams.Info(fmt.Sprintf("Successfully configured Addon repo: %s, please use 'vela addon:update %s' to sync addons", args[0], args[0]))
			return nil
		},
		Annotations: map[string]string{
			types.TagCommandType: types.TypeOthers,
		},
	}
	cmd.PersistentFlags().StringP("token", "t", "", "Github Repo token")
	return cmd
}

func NewAddonInstallCommand(c types.Args, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "addon:install <reponame>/<name>",
		Short:   "Install addon plugin into cluster",
		Long:    "Install addon plugin into cluster",
		Example: `vela addon:install myhub/route`,
		RunE: func(cmd *cobra.Command, args []string) error {
			argsLength := len(args)
			if argsLength < 1 {
				return errors.New("you must specify <reponame>/<name> for addon plugin you want to install")
			}
			newClient, err := client.New(c.Config, client.Options{Scheme: c.Schema})
			if err != nil {
				return err
			}
			ss := strings.Split(args[0], "/")
			if len(ss) < 2 {
				return errors.New("invalid format for " + args[0] + ", please follow format <reponame>/<name>")
			}
			repoName := ss[0]
			name := ss[1]
			return InstallAddonPlugin(newClient, repoName, name, ioStreams)
		},
		Annotations: map[string]string{
			types.TagCommandType: types.TypeOthers,
		},
	}
	cmd.PersistentFlags().StringP("token", "t", "", "Github Repo token")
	return cmd
}

func NewAddonUpdateCommand(ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "addon:update <repoName>",
		Short:   "Update addon repositories, default for all repo",
		Long:    "Update addon repositories, default for all repo",
		Example: `vela addon:update myrepo`,
		RunE: func(cmd *cobra.Command, args []string) error {
			repos, err := plugins.LoadRepos()
			if err != nil {
				return err
			}
			var specified string
			if len(args) > 0 {
				specified = args[0]
			}
			if len(repos) == 0 {
				return fmt.Errorf("no addon repo configured")
			}
			find := false
			if specified != "" {
				for idx, r := range repos {
					if r.Name == specified {
						repos = []plugins.RepoConfig{repos[idx]}
						find = true
						break
					}
				}
				if !find {
					return fmt.Errorf("%s repo not exist", specified)
				}
			}
			ctx := context.Background()
			for _, d := range repos {
				client, err := plugins.NewAddClient(ctx, d.Name, d.Address, d.Token)
				err = client.SyncRemoteAddons()
				if err != nil {
					return err
				}
			}
			return nil
		},
		Annotations: map[string]string{
			types.TagCommandType: types.TypeOthers,
		},
	}
	return cmd
}

func NewAddonListCommand(ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "addon:ls <repoName>",
		Short:   "List addons",
		Long:    "List addons of workloads and traits",
		Example: `vela addon:ls`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var repoName string
			if len(args) > 0 {
				repoName = args[0]
			}
			dir, err := system.GetRepoDir()
			if err != nil {
				return err
			}
			table := uitable.New()
			table.AddRow("NAME", "TYPE", "DEFINITION", "STATUS", "APPLIES-TO")
			if repoName != "" {
				if err = ListRepoAddons(table, filepath.Join(dir, repoName), ioStreams); err != nil {
					return err
				}
				ioStreams.Info(table.String())
				return nil
			}
			dirs, err := ioutil.ReadDir(dir)
			if err != nil {
				return err
			}

			for _, dd := range dirs {
				if !dd.IsDir() {
					continue
				}
				if err = ListRepoAddons(table, filepath.Join(dir, dd.Name()), ioStreams); err != nil {
					return err
				}
			}
			ioStreams.Info(table.String())
			return nil
		},
		Annotations: map[string]string{
			types.TagCommandType: types.TypeOthers,
		},
	}
	return cmd
}

func InstallAddonPlugin(client client.Client, repoName, addonName string, ioStreams cmdutil.IOStreams) error {
	dir, _ := system.GetRepoDir()
	repoDir := filepath.Join(dir, repoName)
	tp, err := GetTemplate(repoName, addonName)
	if err != nil {
		return err
	}
	switch tp.Type {
	case types.TypeWorkload:
		var wd v1alpha2.WorkloadDefinition
		workloadData, err := ioutil.ReadFile(filepath.Join(repoDir, tp.CrdName+".yaml"))
		if err != nil {
			return nil
		}
		if err = yaml.Unmarshal(workloadData, &wd); err != nil {
			return err
		}
		wd.Namespace = types.DefaultOAMNS
		ioStreams.Info("Installing workload plugin " + wd.Name)
		if tp.Install != nil {
			if err = InstallHelmChart(ioStreams, tp.Install.Helm); err != nil {
				return err
			}
		}
		return client.Create(context.Background(), &wd)
	case types.TypeTrait:
		var td v1alpha2.TraitDefinition
		traitdata, err := ioutil.ReadFile(filepath.Join(repoDir, tp.CrdName+".yaml"))
		if err != nil {
			return nil
		}
		if err = yaml.Unmarshal(traitdata, &td); err != nil {
			return err
		}
		td.Namespace = types.DefaultOAMNS
		ioStreams.Info("Installing trait plugin " + td.Name)
		if tp.Install != nil {
			if err = InstallHelmChart(ioStreams, tp.Install.Helm); err != nil {
				return err
			}
		}
		return client.Create(context.Background(), &td)
	}
	return nil
}

func InstallHelmChart(ioStreams cmdutil.IOStreams, charts []types.Chart) error {
	for _, c := range charts {
		if err := HelmInstall(ioStreams, c.Repo, c.URl, c.Repo+"/"+c.Name, c.Version, c.Name); err != nil {
			return err
		}
	}
	return nil
}

func GetTemplate(repoName, addonName string) (types.Template, error) {
	dir, _ := system.GetRepoDir()
	repoDir := filepath.Join(dir, repoName)
	templates, err := plugins.LoadPluginsFromLocal(repoDir)
	if err != nil {
		return types.Template{}, err
	}
	for _, t := range templates {
		if t.Name == addonName {
			return t, nil
		}
	}
	return types.Template{}, fmt.Errorf("%s/%s not exist, try vela addon:update %s to sync from remote", repoName, addonName, repoName)
}

func ListRepoAddons(table *uitable.Table, repoDir string, ioStreams cmdutil.IOStreams) error {
	templates, err := plugins.LoadPluginsFromLocal(repoDir)
	if err != nil {
		return err
	}
	if len(templates) < 1 {
		return nil
	}
	baseDir := filepath.Base(repoDir)
	for _, p := range templates {
		status := CheckInstalled(baseDir, p)
		table.AddRow(baseDir+"/"+p.Name, p.Type, p.Type, status, p.AppliesTo)
	}
	return nil
}

func CheckInstalled(repoName string, tmp types.Template) string {
	var status = "uninstalled"
	dir, _ := system.GetDefinitionDir()
	switch tmp.Type {
	case types.TypeTrait:
		dir = filepath.Join(dir, "traits")
	case types.TypeWorkload:
		dir = filepath.Join(dir, "workloads")
	}
	installed, _ := plugins.LoadTempFromLocal(dir)
	for _, i := range installed {
		//TODO handle source on install
		if i.Source != nil && i.Source.RepoName == repoName && i.Name == tmp.Name && i.CrdName == tmp.CrdName {
			return "installed"
		}
	}
	return status
}
