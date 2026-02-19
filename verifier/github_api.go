package verifier

import (
	"context"
	"errors"
	"fmt"
	"strings"

	gh "github.com/google/go-github/v83/github"
)

func parseRepo(fullRepo string) (string, string, error) {
	fullRepo = strings.TrimSpace(fullRepo)
	parts := strings.Split(fullRepo, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("repo 格式不正确: %s", fullRepo)
	}
	owner := strings.TrimSpace(parts[0])
	repo := strings.TrimSpace(parts[1])
	if owner == "" || repo == "" {
		return "", "", fmt.Errorf("repo 格式不正确: %s", fullRepo)
	}
	return owner, repo, nil
}

func getRepo(ctx context.Context, client *gh.Client, fullRepo string) (*gh.Repository, error) {
	owner, repo, err := parseRepo(fullRepo)
	if err != nil {
		return nil, err
	}
	r, _, err := client.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func getLatestCommit(ctx context.Context, client *gh.Client, fullRepo string) (*gh.RepositoryCommit, error) {
	owner, repo, err := parseRepo(fullRepo)
	if err != nil {
		return nil, err
	}
	commits, _, err := client.Repositories.ListCommits(ctx, owner, repo, &gh.CommitsListOptions{ListOptions: gh.ListOptions{PerPage: 1}})
	if err != nil {
		return nil, err
	}
	if len(commits) == 0 || commits[0] == nil {
		return nil, errors.New("未找到提交记录")
	}
	return commits[0], nil
}

func getLatestCommitSHA(ctx context.Context, client *gh.Client, fullRepo string) (string, error) {
	commit, err := getLatestCommit(ctx, client, fullRepo)
	if err != nil {
		return "", err
	}
	sha := commit.GetSHA()
	if sha == "" {
		return "", errors.New("无法获取最新提交哈希")
	}
	return sha, nil
}

func getLatestRelease(ctx context.Context, client *gh.Client, fullRepo string) (*gh.RepositoryRelease, error) {
	owner, repo, err := parseRepo(fullRepo)
	if err != nil {
		return nil, err
	}
	release, _, err := client.Repositories.GetLatestRelease(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	return release, nil
}

func getFileContent(ctx context.Context, client *gh.Client, fullRepo, path string) (string, error) {
	owner, repo, err := parseRepo(fullRepo)
	if err != nil {
		return "", err
	}
	file, _, _, err := client.Repositories.GetContents(ctx, owner, repo, strings.TrimPrefix(path, "/"), &gh.RepositoryContentGetOptions{})
	if err != nil {
		return "", err
	}
	if file == nil {
		return "", errors.New("未找到文件内容")
	}
	content, err := file.GetContent()
	if err != nil {
		return "", err
	}
	return content, nil
}

func getRepoFileList(ctx context.Context, client *gh.Client, fullRepo, commitSHA string, maxDepth int) ([]string, error) {
	if strings.TrimSpace(commitSHA) == "" {
		return nil, errors.New("commit hash 不能为空")
	}
	if maxDepth <= 0 {
		maxDepth = 2
	}
	owner, repo, err := parseRepo(fullRepo)
	if err != nil {
		return nil, err
	}

	commit, _, err := client.Git.GetCommit(ctx, owner, repo, commitSHA)
	if err != nil {
		return nil, err
	}
	if commit == nil || commit.Tree == nil || commit.Tree.GetSHA() == "" {
		return nil, errors.New("无法获取提交树哈希")
	}

	tree, _, err := client.Git.GetTree(ctx, owner, repo, commit.Tree.GetSHA(), true)
	if err != nil {
		return nil, err
	}
	if tree == nil {
		return nil, errors.New("无法获取仓库文件树")
	}

	files := make([]string, 0, len(tree.Entries))
	for _, item := range tree.Entries {
		if item == nil {
			continue
		}
		if item.GetType() != "blob" {
			continue
		}
		if item.GetPath() == "" {
			continue
		}
		depth := len(strings.Split(item.GetPath(), "/"))
		if depth > maxDepth {
			continue
		}
		files = append(files, item.GetPath())
	}
	return files, nil
}

func isGitHubNotFound(err error) bool {
	var ghErr *gh.ErrorResponse
	if !errors.As(err, &ghErr) {
		return false
	}
	return ghErr.Response != nil && ghErr.Response.StatusCode == 404
}
