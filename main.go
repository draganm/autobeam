package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
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

			ctx, stop := signal.NotifyContext(c.Context, syscall.SIGINT, syscall.SIGKILL, syscall.SIGTERM)
			defer stop()

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

			currentBranch := h.Name().Short()
			isMainBranch := currentBranch == beamConfig.MainBranch

			var imageTag string
			var nextVersion semver.Version

			// Get tags for versioning
			tags, err := repo.Tags()
			if err != nil {
				return fmt.Errorf("failed to get tags: %w", err)
			}

			if isMainBranch {
				// Handle main branch release with semantic versioning
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

				imageTag = fmt.Sprintf("v%s", nextVersion.String())
				fmt.Println("Next version:", nextVersion)
			} else {
				// Handle feature branch with timestamp-based versioning
				timestamp := time.Now().Format("20060102-150405")
				imageTag = fmt.Sprintf("%s-%s", currentBranch, timestamp)
				fmt.Printf("Creating feature branch release with tag: %s\n", imageTag)
			}

			// Check and handle go.mod replace statements
			goModPath := filepath.Join(repoRoot, "go.mod")
			_, err = os.Stat(goModPath)
			if err == nil {
				// Save original go.mod content
				originalGoMod, err := os.ReadFile(goModPath)
				if err != nil {
					return fmt.Errorf("failed to read original go.mod: %w", err)
				}

				// Ensure we restore go.mod regardless of what happens
				defer func() {
					err := os.WriteFile(goModPath, originalGoMod, 0644)
					if err != nil {
						log.Printf("WARNING: failed to restore go.mod: %v", err)
					} else {
						fmt.Println("Restored original go.mod file")
					}

					// Run go mod tidy to ensure go.sum is in sync
					cmd := exec.CommandContext(ctx, "go", "mod", "tidy")
					cmd.Dir = repoRoot
					if err := cmd.Run(); err != nil {
						log.Printf("WARNING: failed to tidy go.mod after restore: %v", err)
					}
				}()

				// Get go.mod content in JSON format
				cmd := exec.CommandContext(ctx, "go", "mod", "edit", "-json")
				cmd.Dir = repoRoot
				output, err := cmd.Output()
				if err != nil {
					return fmt.Errorf("failed to get go.mod json: %w", err)
				}

				// Parse the JSON output
				var goMod struct {
					Module struct {
						Path string `json:"Path"`
					} `json:"Module"`
					Replace []struct {
						Old struct {
							Path    string `json:"Path"`
							Version string `json:"Version"`
						} `json:"Old"`
						New struct {
							Path    string `json:"Path"`
							Version string `json:"Version"`
						} `json:"New"`
					} `json:"Replace"`
				}

				if err := json.Unmarshal(output, &goMod); err != nil {
					return fmt.Errorf("failed to parse go.mod json: %w", err)
				}

				// If there are replace directives
				if len(goMod.Replace) > 0 {
					fmt.Printf("Found %d replace statements in go.mod, handling them...\n", len(goMod.Replace))

					for _, replace := range goMod.Replace {
						fmt.Println("Dropping replace statement for", fmt.Sprintf("%s@%s", replace.Old.Path, replace.Old.Version))
						// Drop all replace statements
						cmd = exec.CommandContext(ctx, "go", "mod", "edit", "-dropreplace", fmt.Sprintf("%s@%s", replace.Old.Path, replace.Old.Version))
						cmd.Dir = repoRoot
						cmd.Stdout = os.Stdout
						cmd.Stderr = os.Stderr
						err = cmd.Run()
						if err != nil {
							return fmt.Errorf("failed to drop replace statements: %w", err)
						}
					}

					// Update each replaced module to @latest

					replaceCommand := []string{"mod", "edit"}

					for _, replace := range goMod.Replace {
						modulePath := replace.Old.Path
						if modulePath != "" && !strings.Contains(modulePath, "internal") {
							fmt.Printf("Updating module %s to @latest\n", modulePath)
							replaceCommand = append(replaceCommand, "-require", fmt.Sprintf("%s@latest", modulePath))
						}

					}
					cmd = exec.CommandContext(ctx, "go", replaceCommand...)
					cmd.Dir = repoRoot
					cmd.Stdout = os.Stdout
					cmd.Stderr = os.Stderr
					err = cmd.Run()
					if err != nil {
						return fmt.Errorf("failed to update replaced modules to @latest: %w", err)
					}

					// Tidy up the go.mod
					cmd = exec.CommandContext(ctx, "go", "mod", "tidy")
					cmd.Dir = repoRoot
					cmd.Stdout = os.Stdout
					cmd.Stderr = os.Stderr
					err = cmd.Run()
					if err != nil {
						return fmt.Errorf("failed to tidy go.mod: %w", err)
					}
				}
			}

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
				"-t", fmt.Sprintf("%s:%s", beamConfig.DockerImage, imageTag),
				".",
			)
			cmd := exec.CommandContext(ctx,
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

			cmd = exec.CommandContext(ctx,
				"docker",
				"push",
				fmt.Sprintf("%s:%s", beamConfig.DockerImage, imageTag),
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

			var branchName string
			if isMainBranch {
				branchName = fmt.Sprintf("autobeam/%s/%s", beamConfig.Name, nextVersion.String())
			} else {
				branchName = fmt.Sprintf("autobeam/%s/%s", beamConfig.Name, imageTag)
			}

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
					"dockerImage": fmt.Sprintf("%s:%s", beamConfig.DockerImage, imageTag),
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

			ghCmd := exec.CommandContext(ctx,
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

			// Only create tag in main repo if on main branch
			if isMainBranch {
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
