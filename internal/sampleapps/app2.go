package sampleapps

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/mmcdole/gofeed"
	"github.com/racingmars/go3270"
)

const (
	skyNewsFeedURL   = "https://feeds.skynews.com/feeds/rss/uk.xml"
	metOfficeFeedURL = "https://www.metoffice.gov.uk/public/data/PWSCache/WarningsRSS/Region/UK"
	ncscFeedURL      = "https://www.ncsc.gov.uk/api/1/services/v1/all-rss-feed.xml"
	bbcFeedURL       = "https://feeds.bbci.co.uk/news/rss.xml"
)

var app2FeedSelectionScreen = go3270.Screen{
	{Row: 0, Col: 27, Intense: true, Content: "RSS Newsreader Application"},
	{Row: 2, Col: 0, Content: "Select the RSS feed to view:"},
	{Row: 4, Col: 0, Content: "(1) Sky UK News"},
	{Row: 5, Col: 0, Content: "(2) Met Office UK Weather"},
	{Row: 6, Col: 0, Content: "(3) NCSC Latest"},
	{Row: 7, Col: 0, Content: "(4) BBC Top Stories"},
	{Row: 10, Col: 0, Content: "Choice:"},
	{Row: 10, Col: 8, Name: "feedChoice", Write: true, Highlighting: go3270.Underscore},
	{Row: 10, Col: 11, Autoskip: true},
	{Row: 22, Col: 0, Content: "PF3 Exit"},
}

func handleApp2(conn net.Conn) {
	defer conn.Close()

	go3270.NegotiateTelnet(conn)

	for {
		response, err := go3270.ShowScreen(app2FeedSelectionScreen, nil, 10, 9, conn)
		if err != nil {
			return
		}

		if response.AID == go3270.AIDPF3 {
			return
		}

		if response.AID == go3270.AIDEnter {
			feedChoice := strings.TrimSpace(response.Values["feedChoice"])
			feedURL, ok := app2FeedURL(feedChoice)
			if !ok {
				continue
			}

			items, err := fetchRSSFeed(feedURL)
			if err != nil {
				continue
			}

			for {
				selection, err := displayHeadlines(conn, items)
				if err != nil {
					break
				}
				if selection == "PF3" {
					break
				}

				selectedIndex, err := strconv.Atoi(selection)
				if err != nil || selectedIndex < 1 || selectedIndex > len(items) {
					continue
				}

				displayDetails(conn, items[selectedIndex-1])
			}
		}
	}
}

func app2FeedURL(choice string) (string, bool) {
	switch choice {
	case "1":
		return skyNewsFeedURL, true
	case "2":
		return metOfficeFeedURL, true
	case "3":
		return ncscFeedURL, true
	case "4":
		return bbcFeedURL, true
	default:
		return "", false
	}
}

func fetchRSSFeed(url string) ([]*gofeed.Item, error) {
	fp := gofeed.NewParser()
	feed, err := fp.ParseURL(url)
	if err != nil {
		return nil, err
	}
	return feed.Items, nil
}

func displayHeadlines(conn net.Conn, items []*gofeed.Item) (string, error) {
	const startRow = 2
	const maxItems = 15

	dynamicHeadlinesScreen := make(go3270.Screen, 0, maxItems+6)

	dynamicHeadlinesScreen = append(dynamicHeadlinesScreen, go3270.Field{
		Row: 0, Col: 0, Intense: true, Content: "Headlines",
	})

	for i, item := range items {
		if i >= maxItems {
			break
		}
		dynamicHeadlinesScreen = append(dynamicHeadlinesScreen, go3270.Field{
			Row: startRow + i, Col: 0, Content: fmt.Sprintf("%d. %s", i+1, item.Title),
		})
	}

	choiceRow := maxItems + 3
	dynamicHeadlinesScreen = append(dynamicHeadlinesScreen,
		go3270.Field{Row: choiceRow, Col: 0, Content: "Choice:"},
		go3270.Field{Row: choiceRow, Col: 8, Name: "selection", Write: true, Highlighting: go3270.Underscore},
		go3270.Field{Row: choiceRow, Col: 11, Autoskip: true},
		go3270.Field{Row: choiceRow + 2, Col: 0, Content: "PF3 Exit"},
	)

	response, err := go3270.ShowScreen(dynamicHeadlinesScreen, nil, choiceRow, 9, conn)
	if err != nil {
		return "", err
	}

	if response.AID == go3270.AIDPF3 {
		return "PF3", nil
	}

	return strings.TrimSpace(response.Values["selection"]), nil
}

func displayDetails(conn net.Conn, item *gofeed.Item) {
	desc := item.Description
	if desc == "" {
		desc = item.Title
	}
	descRows := len(desc) / 80
	if len(desc)%80 != 0 {
		descRows++
	}
	if descRows < 1 {
		descRows = 1
	}

	detailsScreen := make(go3270.Screen, 2+descRows+1)
	detailsScreen[0] = go3270.Field{Row: 0, Col: 0, Content: "Title: " + item.Title, Intense: true}

	for i := 0; i < descRows; i++ {
		startIdx := i * 79
		endIdx := startIdx + 79
		if startIdx >= len(desc) {
			break
		}
		if endIdx > len(desc) {
			endIdx = len(desc)
		}
		detailsScreen[i+1] = go3270.Field{Row: i + 2, Col: 0, Content: desc[startIdx:endIdx]}
	}

	detailsScreen[2+descRows] = go3270.Field{Row: 22, Col: 0, Content: "PF3 - Return"}

	for {
		response, err := go3270.ShowScreen(detailsScreen, nil, 0, 0, conn)
		if err != nil {
			return
		}
		if response.AID == go3270.AIDPF3 {
			break
		}
	}
}
