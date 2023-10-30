// Copyright 2023 The CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/go-github/v56/github"
)

var (
	githubOrg   = envOr("GITHUB_ORG", "cue-labs-modules-testing")
	githubToken = envMust("GITHUB_TOKEN")
)

func main() {
	client := github.NewClient(nil).WithAuthToken(githubToken)
	ctx := context.TODO()

	// Starting from the oldest, delete any repositories until a cutoff point.
	// For now, during active development, we delete all repos regardless of creation time.
moreRepos:
	for {
		repos, resp, err := client.Repositories.ListByOrg(ctx, githubOrg, &github.RepositoryListByOrgOptions{
			Sort:      "created",
			Direction: "asc",
			ListOptions: github.ListOptions{
				Page:    1,
				PerPage: 100,
			},
		})
		if err != nil {
			panic(err)
		}
		cutoff := time.Now()
		anyDeleted := false
		// cutoff := time.Now().Sub(24 * time.Hour)
		for _, repo := range repos {
			if repo.CreatedAt.After(cutoff) {
				break moreRepos // We're past the cutoff point; no more repos to delete.
			}
			repoName := repo.GetName()
			switch repoName {
			case "manual-testing":
				// We want to keep these repos around.
				continue
			}
			log.Printf("deleting %s/%s", githubOrg, repoName)
			if _, err := client.Repositories.Delete(ctx, githubOrg, repoName); err != nil {
				panic(err)
			}
			anyDeleted = true
		}
		// If we didn't find any more repos to delete, or we're at the last page, stop.
		// Note that we always get the first page since we delete repositories from its start.
		if !anyDeleted || resp.NextPage == 0 {
			break
		}
	}
}

func envOr(name, fallback string) string {
	if s := os.Getenv(name); s != "" {
		return s
	}
	return fallback
}

func envMust(name string) string {
	if s := os.Getenv(name); s != "" {
		return s
	}
	panic(fmt.Sprintf("%s must be set", name))
}
