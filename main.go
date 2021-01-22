package main

import (
	"bytes"
	"context"
	"io/ioutil"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"html/template"

	"github.com/google/go-github/v33/github"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	log "github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
	"gopkg.in/yaml.v2"
)

const org = "code-ready"

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

type credentials struct {
	GithubCom []struct {
		User       string `yaml:"user"`
		OauthToken string `yaml:"oauth_token"`
		Protocol   string `yaml:"protocol"`
	} `yaml:"github.com"`
}

type status struct {
	Pendings int
	Failures int
	Total    int
}

type pullRequest struct {
	github.PullRequest
	Status status `json:"status"`
}

type indexTemplate struct {
	PullRequests []pullRequest
	Stats        []float64
}

var stats []float64
var pullRequestsByRepo = make(map[string][]pullRequest)
var lock = sync.Mutex{}

func run() error {
	go func() {
		for {
			if err := dayWorker(); err != nil {
				log.Errorf(err.Error())
			}
			time.Sleep(24 * time.Hour)
		}
	}()
	go func() {
		for {
			if err := minuteWorker(); err != nil {
				log.Errorf(err.Error())
			}
			time.Sleep(time.Minute)
		}
	}()

	e := echo.New()
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.GET("/", func(c echo.Context) error {
		lock.Lock()
		defer lock.Unlock()
		allPRs := make([]pullRequest, 0)
		for _, prs := range pullRequestsByRepo {
			for _, pr := range prs {
				allPRs = append(allPRs, pr)
			}
		}

		sort.Slice(allPRs, func(i, j int) bool {
			return (*allPRs[j].CreatedAt).Before(*allPRs[i].CreatedAt)
		})

		tpl, err := ioutil.ReadFile("public/index.html")
		if err != nil {
			return err
		}
		t, err := template.New("foo").Parse(string(tpl))
		if err != nil {
			return err
		}
		var out bytes.Buffer
		if err = t.Execute(&out, indexTemplate{
			PullRequests: allPRs,
			Stats:        stats,
		}); err != nil {
			return err
		}
		return c.HTML(200, out.String())
	})
	return e.Start(":8080")
}

func minuteWorker() error {
	var credentials credentials
	bin, err := ioutil.ReadFile(os.ExpandEnv("$HOME/.config/hub"))
	if err := yaml.Unmarshal(bin, &credentials); err != nil {
		return err
	}
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: credentials.GithubCom[0].OauthToken},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)
	opt := &github.RepositoryListByOrgOptions{Type: "public"}
	repos, _, err := client.Repositories.ListByOrg(context.Background(), org, opt)
	if err != nil {
		return err
	}

	for _, repo := range repos {
		log.Infof("refreshing %s", *repo.Name)
		prs, _, err := client.PullRequests.List(context.Background(), org, *repo.Name, &github.PullRequestListOptions{})
		if err != nil {
			return err
		}
		allPRs := make([]pullRequest, 0)
		for _, pr := range prs {
			statuses, _, err := client.Repositories.ListStatuses(context.Background(), org, *pr.Base.Repo.Name, *pr.Head.SHA, nil)
			if err != nil {
				return err
			}
			total := 0
			failures := 0
			pendings := 0
			seen := make(map[string]struct{})
			for _, repoStatus := range statuses {
				if strings.Contains(*repoStatus.Context, "centos") {
					continue
				}
				if strings.Contains(*repoStatus.Context, "build_docs") {
					continue
				}
				if _, ok := seen[*repoStatus.Context]; ok {
					continue
				}
				seen[*repoStatus.Context] = struct{}{}
				if *repoStatus.State == "failure" {
					failures++
				}
				if *repoStatus.State == "pending" {
					pendings++
				}
				total++
			}
			allPRs = append(allPRs, pullRequest{
				PullRequest: *pr,
				Status: status{
					Pendings: pendings,
					Failures: failures,
					Total:    total,
				},
			})
		}

		lock.Lock()
		pullRequestsByRepo[*repo.Name] = allPRs
		lock.Unlock()
	}
	return nil
}
