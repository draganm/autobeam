package main

type GitopsRepo struct {
	RepoURL string `yaml:"repo_url"`
	Branch  string `yaml:"branch"`
}

type Config struct {
	Name        string     `yaml:"name"`
	DockerImage string     `yaml:"docker_image"`
	Platforms   []string   `yaml:"platforms"`
	MainBranch  string     `yaml:"main_branch"`
	GitopsRepo  GitopsRepo `yaml:"gitops_repo"`
	PRComment   string     `yaml:"pr_comment"`
}
