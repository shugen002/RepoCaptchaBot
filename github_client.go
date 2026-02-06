package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

var ErrGitHubNotFound = errors.New("github 资源不存在")

const githubCacheTTL = 5 * time.Minute

type GitHubClient struct {
	client  *http.Client
	baseURL string
	token   string

	mu           sync.Mutex
	repoCache    map[string]cacheRepo
	commitCache  map[string]cacheCommit
	releaseCache map[string]cacheRelease
}

type cacheRepo struct {
	data    RepoInfo
	expires time.Time
}

type cacheCommit struct {
	data    CommitInfo
	expires time.Time
}

type cacheRelease struct {
	data    ReleaseInfo
	expires time.Time
	valid   bool
}

type RepoInfo struct {
	Language string
}

type CommitInfo struct {
	AuthorName string
	Message    string
	SHA        string
}

type ReleaseInfo struct {
	Tag string
}

func NewGitHubClient(token string) *GitHubClient {
	return &GitHubClient{
		client:       &http.Client{Timeout: 20 * time.Second},
		baseURL:      "https://api.github.com",
		token:        token,
		repoCache:    make(map[string]cacheRepo),
		commitCache:  make(map[string]cacheCommit),
		releaseCache: make(map[string]cacheRelease),
	}
}

func splitRepo(repo string) (string, string) {
	parts := strings.Split(repo, "/")
	if len(parts) >= 2 {
		return parts[0], parts[1]
	}
	return repo, ""
}

func (c *GitHubClient) GetRepo(ctx context.Context, fullRepo string) (RepoInfo, error) {
	owner, repo, err := parseRepo(fullRepo)
	if err != nil {
		return RepoInfo{}, err
	}
	key := owner + "/" + repo

	c.mu.Lock()
	if cached, ok := c.repoCache[key]; ok && time.Now().Before(cached.expires) {
		info := cached.data
		c.mu.Unlock()
		return info, nil
	}
	c.mu.Unlock()

	var resp struct {
		Language string `json:"language"`
	}
	url := fmt.Sprintf("%s/repos/%s/%s", c.baseURL, owner, repo)
	if err := c.getJSON(ctx, url, &resp); err != nil {
		return RepoInfo{}, err
	}
	info := RepoInfo{Language: resp.Language}

	c.mu.Lock()
	c.repoCache[key] = cacheRepo{data: info, expires: time.Now().Add(githubCacheTTL)}
	c.mu.Unlock()
	return info, nil
}

func (c *GitHubClient) GetLatestCommit(ctx context.Context, fullRepo string) (CommitInfo, error) {
	owner, repo, err := parseRepo(fullRepo)
	if err != nil {
		return CommitInfo{}, err
	}
	key := owner + "/" + repo

	c.mu.Lock()
	if cached, ok := c.commitCache[key]; ok && time.Now().Before(cached.expires) {
		info := cached.data
		c.mu.Unlock()
		return info, nil
	}
	c.mu.Unlock()

	var resp []struct {
		SHA    string `json:"sha"`
		Commit struct {
			Author struct {
				Name string `json:"name"`
			} `json:"author"`
			Message string `json:"message"`
		} `json:"commit"`
	}
	url := fmt.Sprintf("%s/repos/%s/%s/commits?per_page=1", c.baseURL, owner, repo)
	if err := c.getJSON(ctx, url, &resp); err != nil {
		return CommitInfo{}, err
	}
	if len(resp) == 0 {
		return CommitInfo{}, errors.New("未找到提交记录")
	}
	info := CommitInfo{AuthorName: resp[0].Commit.Author.Name, Message: resp[0].Commit.Message, SHA: resp[0].SHA}

	c.mu.Lock()
	c.commitCache[key] = cacheCommit{data: info, expires: time.Now().Add(githubCacheTTL)}
	c.mu.Unlock()
	return info, nil
}

func (c *GitHubClient) GetLatestCommitSHA(ctx context.Context, fullRepo string) (string, error) {
	info, err := c.GetLatestCommit(ctx, fullRepo)
	if err != nil {
		return "", err
	}
	if info.SHA == "" {
		return "", errors.New("无法获取最新提交哈希")
	}
	return info.SHA, nil
}

func (c *GitHubClient) GetLatestRelease(ctx context.Context, fullRepo string) (ReleaseInfo, error) {
	owner, repo, err := parseRepo(fullRepo)
	if err != nil {
		return ReleaseInfo{}, err
	}
	key := owner + "/" + repo

	c.mu.Lock()
	if cached, ok := c.releaseCache[key]; ok && time.Now().Before(cached.expires) && cached.valid {
		info := cached.data
		c.mu.Unlock()
		return info, nil
	}
	c.mu.Unlock()

	var resp struct {
		TagName string `json:"tag_name"`
	}
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", c.baseURL, owner, repo)
	if err := c.getJSON(ctx, url, &resp); err != nil {
		if errors.Is(err, ErrGitHubNotFound) {
			return ReleaseInfo{}, ErrGitHubNotFound
		}
		return ReleaseInfo{}, err
	}
	info := ReleaseInfo{Tag: resp.TagName}

	c.mu.Lock()
	c.releaseCache[key] = cacheRelease{data: info, expires: time.Now().Add(githubCacheTTL), valid: true}
	c.mu.Unlock()
	return info, nil
}

func (c *GitHubClient) GetFileLine(ctx context.Context, fullRepo, path string, line int) (string, error) {
	if line <= 0 {
		return "", errors.New("行号必须大于 0")
	}
	owner, repo, err := parseRepo(fullRepo)
	if err != nil {
		return "", err
	}

	var resp struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	url := fmt.Sprintf("%s/repos/%s/%s/contents/%s", c.baseURL, owner, repo, strings.TrimPrefix(path, "/"))
	if err := c.getJSON(ctx, url, &resp); err != nil {
		return "", err
	}

	if resp.Encoding != "base64" {
		return "", errors.New("不支持的文件编码")
	}

	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(resp.Content, "\n", ""))
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	if line > len(lines) {
		return "", fmt.Errorf("行号超出范围：%d", len(lines))
	}
	return strings.TrimRight(lines[line-1], "\r"), nil
}

func (c *GitHubClient) GetFileContent(ctx context.Context, fullRepo, path string) (string, error) {
	owner, repo, err := parseRepo(fullRepo)
	if err != nil {
		return "", err
	}

	var resp struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	url := fmt.Sprintf("%s/repos/%s/%s/contents/%s", c.baseURL, owner, repo, strings.TrimPrefix(path, "/"))
	if err := c.getJSON(ctx, url, &resp); err != nil {
		return "", err
	}

	if resp.Encoding != "base64" {
		return "", errors.New("不支持的文件编码")
	}

	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(resp.Content, "\n", ""))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (c *GitHubClient) GetRepoFileList(ctx context.Context, fullRepo, commitSHA string, maxDepth int) ([]string, error) {
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

	var commitResp struct {
		Commit struct {
			Tree struct {
				SHA string `json:"sha"`
			} `json:"tree"`
		} `json:"commit"`
	}
	commitURL := fmt.Sprintf("%s/repos/%s/%s/commits/%s", c.baseURL, owner, repo, commitSHA)
	if err := c.getJSON(ctx, commitURL, &commitResp); err != nil {
		return nil, err
	}
	if commitResp.Commit.Tree.SHA == "" {
		return nil, errors.New("无法获取提交树哈希")
	}

	var treeResp struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
	}
	treeURL := fmt.Sprintf("%s/repos/%s/%s/git/trees/%s?recursive=1", c.baseURL, owner, repo, commitResp.Commit.Tree.SHA)
	if err := c.getJSON(ctx, treeURL, &treeResp); err != nil {
		return nil, err
	}

	files := make([]string, 0, len(treeResp.Tree))
	for _, item := range treeResp.Tree {
		if item.Type != "blob" {
			continue
		}
		if item.Path == "" {
			continue
		}
		depth := len(strings.Split(item.Path, "/"))
		if depth > maxDepth {
			continue
		}
		files = append(files, item.Path)
	}
	return files, nil
}

func parseRepo(repo string) (string, string, error) {
	owner, name := splitRepo(repo)
	if owner == "" || name == "" {
		return "", "", fmt.Errorf("repo 格式不正确: %s", repo)
	}
	return owner, name, nil
}

func (c *GitHubClient) getJSON(ctx context.Context, url string, dest interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "repo-captcha-bot")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return ErrGitHubNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("github api 错误: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}

	return json.NewDecoder(resp.Body).Decode(dest)
}
