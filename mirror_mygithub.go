package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

var (
	github_api = "https://api.github.com"

	starred_uri    = "/user/starred"
	user_repos_uri = "/user/repos"

	configFile = flag.String("f", "config.json", "config file")

	cfg Config

	lg = log.New(os.Stdout, "", log.LstdFlags)
)

type Repo struct {
	FullName string `json:"full_name"`
	SSHUrl   string `json:"ssh_url"`
}

func (r Repo) String() string {
	return fmt.Sprintf("%s: %s", r.FullName, r.SSHUrl)
}

type Config struct {
	User        string `json:"user"`
	Token       string `json:"token"`
	RepoRootDir string `json:"repo_root_dir"`
}

func main() {
	flag.Parse()
	if !flag.Parsed() {
		flag.Usage()
		return
	}

	cf, err := os.OpenFile(*configFile, os.O_RDONLY, 0x600)
	if err != nil {
		fmt.Println(err)
		return
	}

	content, _ := ioutil.ReadAll(cf)
	err = json.Unmarshal(content, &cfg)
	if err != nil {
		fmt.Printf("parse config file failed, msg: %v", err)
		return
	}

	if cfg.User == "" || cfg.Token == "" {
		fmt.Println("config.json, user and token can't empty")
		return
	}

	lg.Printf("mirror mygithub start work, config(user: %v, repo_root_dir: %v)", cfg.User, cfg.RepoRootDir)

	if _, err := os.Stat(cfg.RepoRootDir); os.IsNotExist(err) {
		lg.Printf("repos dir not exist, try create: %v", cfg.RepoRootDir)
		err = os.MkdirAll(cfg.RepoRootDir, 0700)
		if err != nil {
			lg.Fatal("create repos dir fail, %v", err)
		}
	}

	os.Chdir(cfg.RepoRootDir)

	syncRepos(fmt.Sprintf("%v/users", cfg.RepoRootDir), user_repos_uri)
	syncRepos(fmt.Sprintf("%v/starred", cfg.RepoRootDir), starred_uri)

}

func fetchApiContent(uri string) (reps []Repo) {
	client := &http.Client{}

	api := github_api + uri

	reps = make([]Repo, 0)

	for {
		lg.Printf("fetch api: %s", api)
		req, err := http.NewRequest("GET", api, nil)
		if err != nil {
			panic(err)
		}

		req.SetBasicAuth(cfg.User, cfg.Token)
		rsp, err := client.Do(req)
		if err != nil {
			lg.Fatalf("fetch api %v response error %v", uri, err)
			return
		}
		defer rsp.Body.Close()

		if rsp.StatusCode != 200 {
			lg.Fatalf("fetch api %v response not 200 (status: %v, msg: %v)", uri, rsp.StatusCode, rsp.Status)
			return
		}

		link := strings.Split(strings.Split(rsp.Header.Get("Link"), ",")[0], ";")

		api = link[0][1 : len(link[0])-1]

		content, err := ioutil.ReadAll(rsp.Body)
		if err != nil {
			panic(err)
		}

		for _, r := range parseRepo(content) {
			reps = append(reps, r)
		}

		if !strings.Contains(link[1], "next") {
			break
		}
	}

	return
}

func parseRepo(content []byte) (repos []Repo) {
	err := json.Unmarshal(content, &repos)
	if err != nil {
		panic(err)
	}
	return
}

func doExec(name string, arg ...string) {
	for i := 0; i < 20; i++ {
		cmd := exec.Command(name, arg...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err := cmd.Run()
		if err != nil {
			lg.Printf("[error] cmd run error %v, error: %v", cmd, err)
			lg.Printf("[retry] try rerun cmd: %v", cmd)
			time.Sleep(time.Millisecond * 200)
			continue
		} else {
			break
		}

	}

}

func syncRepos(rootDir, api_uri string) {
	lg.Printf("sync repos.... rootDir:%s, api_uri: %s", rootDir, api_uri)
	repos := fetchApiContent(api_uri)
	lg.Printf("api_uri: %s, repo count: %v", api_uri, len(repos))
	for _, repo := range repos {
		lg.Printf("sync repo: %v, git url: %v", repo.FullName, repo.SSHUrl)
		localDir := fmt.Sprintf("%v/%v", rootDir, repo.FullName)
		if _, err := os.Stat(localDir); err != nil {
			lg.Printf("local git repo dir not found, try create: %v", localDir)
			err = os.MkdirAll(localDir, 0700)
			if err != nil {
				lg.Fatalf("create local repo dir error: %v", err)
			}

			lg.Printf("git clone repo: %v", repo.FullName)
			doExec("git", "clone", repo.SSHUrl, localDir)
		} else {
			os.Chdir(localDir)
			doExec("git", "reset", "--hard")
			doExec("git", "pull", "--rebase")
		}
	}
}
