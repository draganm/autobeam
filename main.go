package main

import (
	"fmt"

	"github.com/go-git/go-git/v5"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name: "autobeam",
		Action: func(c *cli.Context) error {
			repo, err := git.PlainOpen(".")
			if err != nil {
				return fmt.Errorf("failed to open git repo: %w", err)
			}

			wt, err := repo.Worktree()
			if err != nil {
				return fmt.Errorf("failed to get worktree: %w", err)
			}

			h, err := repo.Head()
			if err != nil {
				return fmt.Errorf("failed to get head: %w", err)
			}

			fmt.Println("Current branch:", h.Name())

			status, err := wt.Status()
			if err != nil {
				return fmt.Errorf("failed to get status: %w", err)
			}
			fmt.Println("is clean", status.IsClean())
			return nil

		},
	}
	app.RunAndExitOnError()
}
