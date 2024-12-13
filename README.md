# Autobeam

Autobeam is a CLI tool that automates the release process for applications using GitOps practices. It handles version management, Docker image building/pushing, and GitOps repository updates in a single command.

## Features

- Automatic semantic versioning (major, minor, patch)
- Docker image building with multi-platform support
- GitOps repository synchronization
- Automated pull request creation
- Git tag management

## Installation

```bash
go install github.com/yourusername/autobeam@latest
```

## Configuration

Create a `.autobeam/config.yaml` file in your repository:

```yaml
name: "your-app-name"
docker_image: "your-registry/image-name"
platforms:
  - "linux/amd64"
  - "linux/arm64"
branch: "main"
gitops_repo:
  repo_url: "github.com/org/gitops-repo"
  branch: "main"
pr_comment: "Additional information to include in PR description"
```

## Usage

```bash
# Create a patch release (default)
autobeam

# Specify release type
autobeam --release-type major
autobeam --release-type minor
autobeam --release-type patch
```

## How It Works

1. Validates the repository is clean and on the correct branch
2. Retrieves the latest version from Git tags
3. Increments the version based on the specified release type
4. Builds and pushes a new Docker image with the version tag
5. Clones the GitOps repository
6. Updates manifests with the new image version
7. Creates a new branch and commits changes
8. Opens a pull request in the GitOps repository
