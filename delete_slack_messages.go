package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// ApiHost of the slack api.
	ApiHost = "https://slack.com/api/"
)

func main() {
	token := flag.String("token", "", "Slack user token.")
	channelID := flag.String("channel", "", "Target channel ID.")
	messageTimestamp := flag.String("timestamp", "", "Target message timestamp. If this is not provided, will delete all Messages in the target channelID.")
	execute := flag.Bool("execute", false, "If you delete messages, please set this flag true. The default mode is dry-run(do not delete messages).")
	flag.Parse()

	c := newClient(*token, *channelID)

	if *messageTimestamp == "" {
		log.Printf("Will delete all Messages in the channel.")
		history := c.getMessages("")
		c.deleteMessages(history, *execute)
	} else {
		log.Printf("Will delete the message posted at %s in the channel ID: %s", *messageTimestamp, *channelID)
		c.deleteMessage(*messageTimestamp, *execute)
	}
	log.Printf("Messages were successfully deleted.")
}

type client struct {
	token      string
	channelID  string
	httpClient *http.Client
}

// newClient builds a slack client using the provided token.
func newClient(token string, channelID string) *client {
	s := &client{
		token:      token,
		channelID:  channelID,
		httpClient: &http.Client{},
	}

	return s
}

type attachment struct {
	Fallback string `json:"fallback"`
	Text     string `json:"text"`
	Pretext  string `json:"pretext"`
	Title    string `json:"title"`
}

type message struct {
	MessageType string `json:"type"`
	UserID      string `json:"user"`
	Text        string `json:"text"`
	Timestamp   string `json:"ts"`
	Attachments []attachment
}

type metadata struct {
	NextCursor string `json:"next_cursor"`
}

type conversationHistory struct {
	Ok           bool       `json:"ok"`
	Messages     []*message `json:"messages"`
	HasMore      bool       `json:"has_more"`
	Metadata     metadata   `json:"response_metadata"`
	ErrorMessage string     `json:"error"`
}

// getMessages gets at most 100 Messages which posted on a specific slack channel.
// This can't call over 50 times in 1 min.
// If you specify a cursor of message history, you can get Messages from the cursor.
// See https://api.slack.com/methods/conversations.history
func (client *client) getMessages(cursor string) conversationHistory {
	values := url.Values{}
	values.Add("token", client.token)
	values.Add("channel", client.channelID)
	if cursor != "" {
		values.Add("cursor", cursor)
	}
	path := ApiHost + "conversations.history"
	body := strings.NewReader(values.Encode())
	res, err := client.postRequest(path, body, "application/x-www-form-urlencoded; charset=UTF-8")
	// 呼び出し制限のため1秒スリープ
	time.Sleep(1 * time.Second)
	if err != nil {
		log.Fatalf("can't send request: %v", err)
	}
	defer res.Body.Close()

	var history conversationHistory
	err = json.NewDecoder(res.Body).Decode(&history)
	if err != nil {
		log.Fatalf("can't parse response body: %v", err)
	}
	return history
}

// deleteMessage deletes a message by messageTimestamp.
// This can't call over 50 times in 1 min.
// See https://api.slack.com/methods/chat.delete
// See https://api.slack.com/messaging/modifying#deleting
func (client *client) deleteMessage(messageTimestamp string, execute bool) {
	if !execute {
		return
	}
	b, err := json.Marshal(map[string]string{
		"channel": client.channelID,
		"ts":      messageTimestamp,
	})
	if err != nil {
		log.Fatalf("can't create json: %v", err)
	}

	path := ApiHost + "chat.delete"
	body := bytes.NewReader(b)
	res, err := client.postRequest(path, body, "application/json; charset=UTF-8")
	// 呼び出し制限のため1秒スリープ
	time.Sleep(1 * time.Second)
	if err != nil {
		log.Fatalf("can't send request: %v", err)
	}
	defer res.Body.Close()

	bodyBytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		log.Fatalf("can't parse response body: %v", err)
	}
	log.Printf("body: %s", string(bodyBytes))
}

func (client *client) deleteMessages(history conversationHistory, execute bool) {
	if !history.Ok {
		log.Fatalf("can't get messages: %s", history.ErrorMessage)
	}

	for _, message := range history.Messages {
		if message.Text != "" {
			log.Printf("delete a message: %s", message.Text)
		} else if len(message.Attachments) != 0 {
			log.Printf("delete a message: %s", message.Attachments[0].Title)
		}
		// 取得したメッセージ一覧を1件ずつ削除
		client.deleteMessage(message.Timestamp, execute)
	}

	// 次のメッセージがあったら次のcursorを見て再度メッセージ取得・削除
	if history.HasMore {
		log.Printf("next cursor: %s", history.Metadata.NextCursor)
		next := client.getMessages(history.Metadata.NextCursor)
		client.deleteMessages(next, execute)
	}
}

func (client *client) postRequest(path string, body io.Reader, contentType string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, path, body)
	if err != nil {
		log.Fatalf("can't build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+client.token)
	req.Header.Set("Content-Type", contentType)
	return client.httpClient.Do(req)
}
