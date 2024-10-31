package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"

	"github.com/Masterminds/semver/v3"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/urfave/cli/v2"

	"gopkg.in/yaml.v3"
)

func main() {
	cfg := struct {
		releaseType string
	}{}
	app := &cli.App{
		Name: "autobeam",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "release-type",
				Usage:       "release type",
				EnvVars:     []string{"RELEASE_TYPE"},
				Destination: &cfg.releaseType,
				Value:       "patch",
			},
		},

		Action: func(c *cli.Context) error {

			// 5. Get the latest semver from tags
			// 6. Increment the patch version
			// 7. Build the docker image with the new version
			// 8. Push the new image to the registry
			// 9. Create a new tag with the new version
			// 10. clone the gitops repo in memory
			// 11. create a new branch in the gitops repo
			// 12. generate the new version of manifests
			// 13. commit the new version of manifests
			// 14. push the new branch to the gitops repo
			// 15. create a PR
			// 16. create new tag in the main repo
			// 17. push the new tag to the main repo
			// 18. show the PR link

			// 1. Open the repo
			dir, err := filepath.Abs(".")
			if err != nil {
				return fmt.Errorf("failed to get absolute path of the current dir: %w", err)
			}

			repoRoot, err := findRepositoryRoot(dir)
			if err != nil {
				return fmt.Errorf("failed to find repository root: %w", err)
			}

			repo, err := git.PlainOpen(repoRoot)
			if err != nil {
				return fmt.Errorf("failed to open git repo: %w", err)
			}

			// 2. check if the repo is clean

			wt, err := repo.Worktree()
			if err != nil {
				return fmt.Errorf("failed to get worktree: %w", err)
			}

			status, err := wt.Status()
			if err != nil {
				return fmt.Errorf("failed to get status: %w", err)
			}

			if !status.IsClean() {
				return fmt.Errorf("%s\nworking tree is not clean, please commit changes", status.String())
			}

			// 3. Load the config file from the repo

			configFile, err := wt.Filesystem.Open(".autobeam/config.yaml")
			if err != nil {
				return fmt.Errorf("failed to open config file: %w", err)
			}

			beamConfig := &Config{}
			defer configFile.Close()
			err = yaml.NewDecoder(configFile).Decode(beamConfig)
			if err != nil {
				return fmt.Errorf("failed to decode config file: %w", err)
			}

			// 4. Check if the branch in the config file is the same as the current branch

			h, err := repo.Head()
			if err != nil {
				return fmt.Errorf("failed to get head: %w", err)
			}

			if h.Name().Short() != beamConfig.Branch {
				return fmt.Errorf("current branch %q is not the same as the branch in the config: %q", h.Name().Short(), beamConfig.Branch)
			}

			// 5. Check if current branch is pushed to the remote

			remotes, err := repo.Remotes()
			if err != nil {
				return fmt.Errorf("failed to list remotes: %w", err)
			}

			branchPushed := false
			sshAuth, err := getSSHAgentAuth()
			if err != nil {
				return fmt.Errorf("failed to get ssh agent auth: %w", err)
			}

			for _, remote := range remotes {
				listOptions := &git.ListOptions{}
				listOptions.Auth = sshAuth
				refs, err := remote.List(listOptions)
				if err != nil {
					fmt.Printf("Error listing remote refs: %s\n", err)
					os.Exit(1)
				}

				for _, ref := range refs {
					if ref.Name() == h.Name() {
						branchPushed = true
						break
					}
				}
				if branchPushed {
					break
				}
			}

			if !branchPushed {
				return fmt.Errorf("current branch %q is not pushed to the remote", h.Name().Short())
			}

			// 6. Get the latest semver from tags

			fmt.Println("Current branch:", h.Name())

			tags, err := repo.Tags()
			if err != nil {
				return fmt.Errorf("failed to get tags: %w", err)
			}

			semverTags := semver.Collection{}
			err = tags.ForEach(func(r *plumbing.Reference) error {
				v, err := semver.NewVersion(r.Name().Short())
				if err == nil {
					semverTags = append(semverTags, v)
					return nil
				}
				log.Println("skipping tag", "tag", r.Name().Short(), "error", err)
				return nil
			})
			if err != nil {
				return fmt.Errorf("failed to iterate tags: %w", err)
			}

			if len(semverTags) == 0 {
				semverTags = append(semverTags, semver.MustParse("v0.0.0"))
			}

			sort.Sort(semverTags)

			latestVersion := semverTags[len(semverTags)-1]
			fmt.Println("Latest version:", latestVersion)

			var nextVersion semver.Version

			switch cfg.releaseType {
			case "major":
				nextVersion = latestVersion.IncMajor()
			case "minor":
				nextVersion = latestVersion.IncMinor()
			case "patch":
				nextVersion = latestVersion.IncPatch()
			default:
				return fmt.Errorf("invalid release type: %q", cfg.releaseType)
			}

			fmt.Println("Next version:", nextVersion)

			return nil

		},
	}
	app.RunAndExitOnError()
}

func findRepositoryRoot(dir string) (string, error) {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no git repository found")
		}
		dir = parent
	}
}
func getSSHAgentAuth() (transport.AuthMethod, error) {
	b, err := ssh.DefaultAuthBuilder("git")
	if err != nil {
		return nil, fmt.Errorf("failed to create default auth builder: %w", err)
	}
	return b, nil

}
