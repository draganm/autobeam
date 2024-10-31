package main

type GitopsRepo struct {
	RepoURL string `yaml:"repo_url"`
	Branch  string `yaml:"branch"`
}

type Config struct {
	Name        string     `yaml:"name"`
	DockerImage string     `yaml:"docker_image"`
	Branch      string     `yaml:"branch"`
	GitopsRepo  GitopsRepo `yaml:"gitops_repo"`
}
