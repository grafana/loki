package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type auth struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func logAndQuit(fmt string, args ...interface{}) {
	log.Printf(fmt, args...)
	os.Exit(0)
}

func main() {
	var (
		auth auth

		repo     string
		maxAge   time.Duration
		filter   string
		doDelete bool
	)

	flag.BoolVar(&doDelete, "delete", false, "turn deletions on")
	flag.StringVar(&auth.Username, "username", "", "username for docker hub")
	flag.StringVar(&auth.Password, "password", "", "password for docker hub")
	flag.StringVar(&repo, "repo", "grafana/loki", "repo to delete tags for")
	flag.StringVar(&filter, "filter", "master-", "delete tags only containing this prefix")
	flag.DurationVar(&maxAge, "max-age", 24*time.Hour*90, "delete tags older than this age")
	flag.Parse()

	if username := os.Getenv("DOCKER_USERNAME"); username != "" {
		auth.Username = username
	}
	if password := os.Getenv("DOCKER_PASSWORD"); password != "" {
		auth.Password = password
	}

	log.Printf("Using repo %s\n", repo)
	log.Printf("Using search filter %s\n", filter)
	log.Printf("Using max age %v\n", maxAge)

	// Get an auth token
	jwt, err := getJWT(auth)
	if err != nil {
		logAndQuit(err.Error())
	}

	tags, err := getTags(jwt, repo)
	if err != nil {
		logAndQuit(err.Error())
	}

	log.Printf("Discovered %d tags pre-filtering\n", len(tags))

	filtered := make([]tag, 0, len(tags))

	for _, t := range tags {
		if !strings.HasPrefix(t.Name, filter) {
			continue
		}
		age := time.Since(t.LastUpdated)
		if age < maxAge {
			continue
		}

		filtered = append(filtered, t)
	}

	if !doDelete {
		log.Printf("Should delete %d tags\n", len(filtered))
		for _, t := range filtered {
			fmt.Printf("%s: last updated %s\n", t.Name, t.LastUpdated)
		}
	} else {
		log.Printf("Deleting %d tags\n", len(filtered))
		for _, t := range filtered {
			log.Printf("Deleting %s (last updated %s)\n", t.Name, t.LastUpdated)
			if err := deleteTag(jwt, repo, t.Name); err != nil {
				log.Printf("Failed to delete %s: %v", t.Name, err)
			}
		}
	}
}

func getJWT(a auth) (string, error) {
	body, err := json.Marshal(a)
	if err != nil {
		return "", err
	}

	loginURL := "https://hub.docker.com/v2/users/login"
	resp, err := http.Post(loginURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
		resp.Body.Close()
		return "", fmt.Errorf("failed to log in: %v", string(body))
	}
	defer resp.Body.Close()

	m := map[string]interface{}{}
	err = json.NewDecoder(resp.Body).Decode(&m)
	if err != nil {
		return "", err
	}

	return m["token"].(string), nil
}

type tag struct {
	Name        string    `json:"name"`
	LastUpdated time.Time `json:"last_updated"`
}

type getTagResponse struct {
	NextURL *string `json:"next"`
	Results []tag   `json:"results"`
}

func getTags(jwt string, repo string) ([]tag, error) {
	var tags []tag

	tagsURL := fmt.Sprintf("https://hub.docker.com/v2/repositories/%s/tags", repo)
	res, err := getTagsFromURL(jwt, tagsURL)
	if err != nil {
		return nil, err
	}
	tags = append(tags, res.Results...)

	for res.NextURL != nil {
		res, err = getTagsFromURL(jwt, *res.NextURL)
		if err != nil {
			return nil, err
		}
		tags = append(tags, res.Results...)
	}

	return tags, nil
}

func getTagsFromURL(jwt string, url string) (getTagResponse, error) {
	var res getTagResponse

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return res, err
	}
	req.Header.Add("Authorization", "JWT "+jwt)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return res, err
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		bb, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return res, err
		}
		return res, errors.New(string(bb))
	}

	err = json.NewDecoder(resp.Body).Decode(&res)
	return res, err
}

func deleteTag(jwt string, repo string, tag string) error {
	tagsURL := fmt.Sprintf("https://hub.docker.com/v2/repositories/%s/tags/%s/", repo, tag)
	req, err := http.NewRequest(http.MethodDelete, tagsURL, nil)
	if err != nil {
		return err
	}
	req.Header.Add("Authorization", "JWT "+jwt)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		bb, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		return errors.New(string(bb))
	}

	return nil
}
