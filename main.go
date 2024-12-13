package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/draganm/autobeam/interpolatemanifests"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-git/go-git/v5/storage/memory"
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

			// 7. Increment the version
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

			// 8. Build the docker image with the new version

			buildArgs := []string{
				"build",
			}

			if len(beamConfig.Platforms) > 0 {
				buildArgs = append(
					buildArgs,
					"--platform",
					strings.Join(beamConfig.Platforms, ","),
				)
			}

			buildArgs = append(
				buildArgs,
				"-t", fmt.Sprintf("%s:v%s", beamConfig.DockerImage, nextVersion.String()),
				".",
			)
			cmd := exec.Command(
				"docker",
				buildArgs...,
			)

			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Dir = repoRoot

			err = cmd.Run()
			if err != nil {
				return fmt.Errorf("failed to build docker image: %w", err)
			}

			// 9. Push the new image to the registry

			cmd = exec.Command(
				"docker",
				"push",
				fmt.Sprintf("%s:v%s", beamConfig.DockerImage, nextVersion.String()),
			)

			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			err = cmd.Run()

			if err != nil {
				return fmt.Errorf("failed to push docker image: %w", err)
			}

			// 10. clone the gitops repo in memory

			workspace := memfs.New()
			gitopsRepo, err := git.Clone(memory.NewStorage(), workspace, &git.CloneOptions{
				URL:           beamConfig.GitopsRepo.RepoURL,
				ReferenceName: plumbing.NewBranchReferenceName(beamConfig.GitopsRepo.Branch),
			})
			if err != nil {
				return fmt.Errorf("failed to clone gitops repo: %w", err)
			}

			opsWT, err := gitopsRepo.Worktree()
			if err != nil {
				return fmt.Errorf("failed to get worktree: %w", err)
			}

			// 11. create a new branch in the gitops repo

			branchName := fmt.Sprintf("autobeam/%s/%s", beamConfig.Name, nextVersion.String())

			err = opsWT.Checkout(&git.CheckoutOptions{
				Branch: plumbing.NewBranchReferenceName(branchName),
				Create: true,
			})

			if err != nil {
				return fmt.Errorf("failed to create branch: %w", err)
			}

			err = interpolatemanifests.RollOut(
				filepath.Join(repoRoot, ".autobeam/manifests"),
				map[string]any{
					"dockerImage": fmt.Sprintf("%s:v%s", beamConfig.DockerImage, nextVersion.String()),
				},
				opsWT.Filesystem,
			)
			if err != nil {
				return fmt.Errorf("failed to interpolate manifests: %w", err)
			}

			opsWT.Filesystem.MkdirAll("test", 0755)
			f, err := opsWT.Filesystem.OpenFile("test/test.txt", os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}
			f.Write([]byte("hello world"))
			f.Close()

			_, err = opsWT.Add(".")
			if err != nil {
				return fmt.Errorf("failed to add files: %w", err)
			}

			_, err = opsWT.Commit("commit message", &git.CommitOptions{
				Author: &object.Signature{
					Name:  "autobeam",
					Email: "autobeam@emal.me",
					When:  time.Now(),
				},
			})

			if err != nil {
				return fmt.Errorf("failed to commit: %w", err)
			}

			// 12. generate the new version of manifests

			// 13. commit the new version of manifests

			// 14. push the new branch to the gitops repo

			err = gitopsRepo.Push(&git.PushOptions{})

			if err != nil {
				return fmt.Errorf("failed to push: %w", err)
			}

			// 15. create a PR

			prComment := fmt.Sprintf("Release %s %s\n---\n%s\n\nGenerated by Autobeam", beamConfig.Name, nextVersion.String(), beamConfig.PRComment)

			ghCmd := exec.Command(
				"gh",
				"pr",
				"create",
				"--repo", beamConfig.GitopsRepo.RepoURL,
				"--base", beamConfig.GitopsRepo.Branch,
				"--head", branchName,
				"--title", fmt.Sprintf("Release %s %s", beamConfig.Name, nextVersion.String()),
				"--body", prComment,
			)

			ghCmd.Stdout = os.Stdout
			ghCmd.Stderr = os.Stderr

			err = ghCmd.Run()
			if err != nil {
				return fmt.Errorf("failed to create PR: %w", err)
			}

			_, err = repo.CreateTag("v"+nextVersion.String(), h.Hash(), &git.CreateTagOptions{
				Tagger: &object.Signature{
					Name:  "autobeam",
					Email: "autobeam@emal.me",
					When:  time.Now(),
				},
				Message: "Release " + nextVersion.String(),
			})

			if err != nil {
				return fmt.Errorf("failed to create tag: %w", err)
			}

			err = repo.Push(&git.PushOptions{
				FollowTags: true,
			})

			if err != nil {
				return fmt.Errorf("failed to push tag: %w", err)
			}

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
