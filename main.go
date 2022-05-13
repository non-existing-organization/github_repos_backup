package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"repo-backup/utils"

	"path/filepath"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
	"golang.org/x/term"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go/aws/awserr"
)

var wg sync.WaitGroup

func clone(url string, directory string, access_token string) {
	defer wg.Done()
	utils.Info("git clone %s %s", url, directory)

	r, err := git.PlainClone(directory, false, &git.CloneOptions{
		Auth: &http.BasicAuth{
			Username: "blah",
			Password: access_token,
		},
		URL:      url,
		Progress: os.Stdout,
	})
	utils.CheckIfError(err)

	// ... retrieving the branch being pointed by HEAD
	if r == nil {
		return
	} else {
		ref, err := r.Head()
		utils.CheckIfError(err)
		// ... retrieving the commit object
		commit, err := r.CommitObject(ref.Hash())
		utils.CheckIfError(err)

		fmt.Println(commit)
	}
}

func listRepositories(access_token string) []*github.Repository {

	opt := &github.RepositoryListOptions{
		ListOptions: github.ListOptions{PerPage: 40},
	}
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: access_token},
	)
	tc := oauth2.NewClient(ctx, ts)

	client := github.NewClient(tc)

	// list all repositories for the authenticated user
	repos, _, err := client.Repositories.List(ctx, "", opt)

	if err != nil {
		fmt.Println(err.Error())
	}

	return repos

}

func returnCreds(region string) aws.Config {
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(region))
	if err != nil {
		log.Fatalf("unable to load SDK config, %v", err)
	}
	return cfg
}

func uploadFileToS3(s3BucketName string, fileName string) *s3.PutObjectOutput {
	svc := s3.NewFromConfig(returnCreds("us-east-1"))
	input := &s3.PutObjectInput{
		Body:    strings.NewReader(fileName),
		Bucket:  aws.String(s3BucketName),
		Key:     aws.String(fileName),
		Tagging: aws.String("Purpose=Created from repo-backup tool"),
	}

	result, err := svc.PutObject(context.TODO(), input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			default:
				fmt.Println(aerr.Error())
			}
		} else {
			fmt.Println(err.Error())
		}
	} else {
		fmt.Printf("Object %s uploaded", fileName)
	}

	return result
}

func main() {
	var directory string
	var outFileName string
	var filter string
	var s3bucket string
	var s3bucketKey string
	var timestamp int32 = int32(time.Now().Unix())
	var timestampString string = strconv.FormatInt(int64(timestamp), 10)

	flag.StringVar(&directory, "d", "", "Specify path to destination to download repos")
	flag.StringVar(&filter, "f", "", "Specify filter to match just a certain amount of repos")
	flag.StringVar(&outFileName, "o", "", "Specify the resulting name of the zip file")
	flag.StringVar(&s3bucket, "s3", "", "Specify the S3	bucket to store the zipped repos")
	flag.StringVar(&s3bucketKey, "s3key", "", "Specify the S3 bucket path to store the zipped repos")
	flag.Parse()

	fmt.Print("Personal Access Token: ")
	fmt.Println()
	bytepw, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		os.Exit(1)
	}
	token := string(bytepw)
	repos := listRepositories(token)
	directory = directory + timestampString
	outFileName = outFileName + timestampString + ".zip"

	for i := range repos {
		if len(filter) == 0 {
			//clone(repos[i].GetCloneURL(), filepath.Join(directory, *repos[i].FullName), token)
			go clone(repos[i].GetCloneURL(), filepath.Join(directory, *repos[i].FullName), token)
			wg.Add(1)
		} else {
			if strings.Contains(repos[i].GetName(), filter) {
				//clone(repos[i].GetCloneURL(), filepath.Join(directory, *repos[i].FullName), token)
				go clone(repos[i].GetCloneURL(), filepath.Join(directory, *repos[i].FullName), token)
				wg.Add(1)
			}
		}
	}

	wg.Wait()

	fmt.Println("zipping repos at destination path....")
	if err := utils.ZipSource(directory, outFileName); err != nil {
		log.Fatal(err.Error())
	}
	if len(s3bucket) != 0 {
		if len(s3bucketKey) != 0 {
			uploadFileToS3(s3bucket, filepath.Join(s3bucketKey, outFileName))
		} else {
			uploadFileToS3(s3bucket, outFileName)
		}
	}
}
