package main

import (
	"fmt"
	"os"
	"testing"
)

func TestTweetExtractor(t *testing.T) {
	fd, err := os.Open("last_tweet.html")
	if err != nil {
		return
	}
	seen := map[string]bool{}
	tweets := tweetExtractor(fd, "last_tweet.html", seen)
	fmt.Printf("tweets: %s\n", tweets)
}
