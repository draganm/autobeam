# Autobeam

Autobeam is a CLI tool that automates the release process for applications using GitOps practices. It handles version management, Docker image building/pushing, and GitOps repository updates in a single command.

## Features

- Automatic semantic versioning (major, minor, patch) for main branch releases
- Feature branch releases with timestamp-based versioning
- Smart handling of Go dependencies during builds:
  - Automatically handles `replace` directives in go.mod
  - Updates replaced modules to @latest versions during build
  - Safely restores original go.mod state after build
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
main_branch: "main"  # Branch to use for semantic versioning
gitops_repo:
  repo_url: "github.com/org/gitops-repo"
  branch: "main"
pr_comment: "Additional information to include in PR description"
```

## Usage

```bash
# Create a patch release (default) when on main branch
autobeam

# Specify release type (only applies to main branch)
autobeam --release-type major
autobeam --release-type minor
autobeam --release-type patch
```

## How It Works

### Main Branch Releases
When running on the main branch (specified by `main_branch` in config):
1. Uses semantic versioning for releases
2. Creates and pushes git tags
3. Creates versioned Docker images (e.g., `v1.2.3`)

### Feature Branch Releases
When running on any other branch:
1. Uses timestamp-based versioning
2. Creates Docker images tagged with `branch-name-timestamp`
3. No git tags are created

### Go Module Handling
During the Docker build process:
1. Temporarily removes `replace` directives from go.mod
2. Updates replaced modules to their @latest versions
3. Builds the Docker image with clean dependencies
4. Automatically restores the original go.mod state after build
   - Restores all replace directives
   - Runs go mod tidy to ensure go.sum is in sync
   - Restoration happens whether build succeeds or fails

### GitOps Integration
1. Clones the GitOps repository
2. Updates manifests with the new image version
3. Creates a new branch and commits changes
4. Opens a pull request in the GitOps repository
