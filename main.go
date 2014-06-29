package main

import (
	"encoding/json"
	"decoding/json"
	"fmt"
	"log"
	"net/textproto"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

const SLEEP = 500

// global vars
var (
	//	err           error
	//	secret        string
Trace         *log.Logger
	Error         *log.Logger
	debug         = true
	privMsg       = regexp.MustCompile("^:([a-zA-Z0-9`_\\-]+)!.+@(.+)PRIVMSG (#[a-zA-Z0-9_]+) :(.*)$")
	directMsg     = regexp.MustCompile("^:([a-zA-Z0-9`_\\-]+)!.+@(.+)PRIVMSG ([a-zA-Z0-9_]+) :(.*)$")
	ping          = regexp.MustCompile("^PING :([a-zA-Z0-9\\.]+)$")
	motd          = regexp.MustCompile("^:[a-zA-Z\\.]+\\s376(.*)") // end of motd
	nickCollision = regexp.MustCompile("^:[a-zA-Z\\.]+\\s433(.*)")
	pCommand      = regexp.MustCompile("^!(\\w+)\\s*(.*)")
)

type users map[string]string

type Configuration struct {
	Nick     string
	RealName string
	Host     string
	Port     string
	Channels []string
	Admins   []User
}

type User struct {
	Name string
	Host string
	Pass string
}

/**
A Message can be send to a channel or a specified user
*/
type IrcMessage struct {
	sender   string
	receiver string
	host     string
	message  string
}

type Command struct {
	sender     string
	receiver   string
	answerTo   string
	command    string
	rawParam   string
	rawCommand string
	isPrivate  bool
}

type Bot struct {
	activeAdmins map[string]bool
	suggestions  map[int]Title
	conn         *textproto.Conn
	writeChan    chan *IrcMessage
	config       *Configuration
}

type Title struct {
	titleSuggestion string
	numOfVotes      int
}

func (bot Bot) sendMessage(receiver, message string) {
	bot.writeChan <- &IrcMessage{
		sender:   "",
		receiver: receiver,
		message:  message,
	}
}

func (bot Bot) parseLine(line string) {
	if debug {
		fmt.Printf("received: %s\n", line)
	}

	if match := nickCollision.FindStringSubmatch(line); match != nil {
		bot.nickCollisionHandler()
	}

	if match := privMsg.FindStringSubmatch(line); match != nil {
		msg := new(IrcMessage)
		msg.sender, msg.host, msg.receiver, msg.message = match[1], match[2], match[3], match[4]
		bot.messageHandler(*msg)
	}
	if match := directMsg.FindStringSubmatch(line); match != nil {
		msg := new(IrcMessage)
		msg.sender, msg.host, msg.receiver, msg.message = match[1], match[2], match[3], match[4]
		bot.directMessageHandler(*msg)
	}
	if match := ping.FindStringSubmatch(line); match != nil {
		bot.pingHandler(match[1])
	}
	if match := motd.FindStringSubmatch(line); match != nil {
		bot.endOfMOTDHandler()
	}
}

func (bot Bot) nickCollisionHandler() {
	if debug {
		fmt.Printf("DEBUG: Nick collision detected (%s). Improvising!\n", bot.config.Nick)
	}
	bot.config.Nick = bot.config.Nick + "`"
	bot.conn.Cmd("NICK %s\r\n", bot.config.Nick)
}
func (bot Bot) endOfMOTDHandler() {
	for i, c := range bot.config.Channels {
		bot.conn.Cmd("JOIN %s\r\n", c)
		if debug {
			fmt.Printf("... joining channel %s \n", c)
		}
		if i%2 == 0 {
			time.Sleep(SLEEP * time.Millisecond)
		}
	}

}
func (bot Bot) pingHandler(payload string) {
	bot.conn.Cmd("PONG :%s\r\n", payload)
}
func (bot Bot) directMessageHandler(msg IrcMessage) {
	if match := pCommand.FindStringSubmatch(msg.message); match != nil {
		if debug {
			fmt.Printf("Command found: %s from %s\n", msg.message, msg.sender)
		}

		bot.handelCommand(Command{
			sender:     msg.sender,
			receiver:   msg.receiver,
			answerTo:   msg.sender,
			rawCommand: msg.message,
			rawParam:   match[2],
			command:    match[1],
			isPrivate:  true,
		})
	}
}
func (bot Bot) messageHandler(msg IrcMessage) {
	if match := pCommand.FindStringSubmatch(msg.message); match != nil {
		if debug {
			fmt.Printf("DEBUG: %s", msg)
		}
		bot.handelCommand(Command{
			sender:     msg.sender,
			receiver:   msg.receiver,
			answerTo:   msg.receiver, // send back to channel
			rawCommand: msg.message,
			rawParam:   match[2],
			command:    match[1],
			isPrivate:  false,
		})
	}
}
func (bot Bot) handelCommand(cmd Command) {
	switch cmd.command {
	case "say":
		if cmd.isPrivate {
			bot.sendMessage(bot.channel, cmd.rawParam)
		}
	case "show":
		if bot.isAdmin(cmd.sender) {
			keys := make([]int, 0, len(bot.suggestions))
			for k := range bot.suggestions {
				keys = append(keys, k)
			}
			sort.Ints(keys)
			for index := range keys {
				text := fmt.Sprintf("ID: %d Titel: %s (%d)", index, bot.suggestions[index].titleSuggestion, bot.suggestions[index].numOfVotes)
				bot.sendMessage(cmd.answerTo, text)
				fmt.Printf("-> %s Msg: %s\n", cmd.answerTo, text)
				time.Sleep(SLEEP * time.Millisecond)
			}
		}
	case "title":
		fallthrough
	case "topic":
		bot.handleTitleSuggestion(cmd)
	case "obeyMe":
		if cmd.isPrivate {
			bot.handleLogin(cmd)
		}
	case "quit":
		if cmd.isPrivate && bot.isAdmin(cmd.sender) {
			bot.conn.Cmd("QUIT %s", cmd.rawParam)
		}
	case "logout":
		if cmd.isPrivate && bot.isAdmin(cmd.sender) {
			fmt.Printf("User %s has been logged out\n", cmd.sender)
			delete(bot.activeAdmins, cmd.sender)
		}
	case "addMaster":
		if cmd.isPrivate && bot.isAdmin(cmd.sender) {
			data := strings.Split(cmd.rawParam, " ")
			if len(data) != 2 {
				break
			}
			newAdmin := User{
				Name: data[0],
				Pass: data[1],
			}

			bot.config.Admins = append(bot.config.Admins, newAdmin)
			fmt.Println(bot.config)
		}
	case "join":
		if cmd.isPrivate && bot.isAdmin(cmd.sender) {
			bot.conn.Cmd("JOIN %s", cmd.rawParam)
		}
	case "part":
		if cmd.isPrivate && bot.isAdmin(cmd.sender) {
			data := strings.Split(cmd.rawParam, " ")
			bot.conn.Cmd("PART %s %s", "#"+data[0], data[1])
		}
	case "fisch":
		bot.sendMessage(cmd.answerTo, "Fischers Frize hat blaue Brautkleider.")
	case "help":
		bot.showHelp(cmd.receiver)
	}
}

func (bot Bot) handleTitleSuggestion(cmd Command) {
	pos := int(len(bot.suggestions))
	bot.suggestions[pos] = Title{
		numOfVotes:      1,
		titleSuggestion: cmd.rawParam,
	}
	fmt.Printf("%s", bot.suggestions)
}

func (bot Bot) handleLogin(cmd Command) {
	fmt.Printf("Login attemt detected %s\n", cmd.sender)
	for _, user := range bot.config.Admins {
		fmt.Printf("User %s Pass %s -> Sender %s Pass %s\n", user.Name, user.Pass, cmd.sender, cmd.rawParam)
		if user.Name == cmd.sender && user.Pass == cmd.rawParam {
			fmt.Printf("Login successfull for %s\n", cmd.sender)
			bot.activeAdmins[user.Name] = true
			bot.sendMessage(cmd.answerTo, "Yes my lord, How may I be at your service?")
			break
		}
	}
	fmt.Printf("Login NOT successfull for %s\n", cmd.sender)
}

func (bot Bot) isAdmin(nick string) bool {
	if bot.activeAdmins[nick] {
		return true
	}
	return false
}

func (bot Bot) setNick() {
	bot.conn.Cmd("USER %s 8 * :%s\r\n", bot.config.Nick, bot.config.Nick)
	bot.conn.Cmd("NICK %s\r\n", bot.config.Nick)
}

func (bot Bot) channelWriter() {
	// read from channel
	msg := <-bot.writeChan
	// todo flood protection here
	time.Sleep(SLEEP * time.Millisecond) // don't flood the server
	bot.conn.Cmd("PRIVMSG %s :%s\r\n", msg.receiver, msg.message)
}

func (bot Bot) startBot() {
	bot.setNick()
	for {
		text, err := bot.conn.ReadLine()
		if err != nil {
			fmt.Printf("%s", err)
			return
		}
		go bot.channelWriter()
		go bot.parseLine(text)
	}
}
func (bot Bot) showHelp(receiver string) {
	var help = []string{
		"- !topic <topic string>   : * Suggest a topic",
		"- !title <topic string>   : * Suggest a topic",
		"- !obeyMe <pass string>   : Will only be recognised if sent as a direct message. Used to authenticate a user with a provided password",
	}
	var adminHelp = []string{
		"-",
		"After a user has been successful authenticated he has access to a admin commands. Each admin command *must* be send as a direct message",
		"- !resetTopicList            : Flushes the topic list",
		"- !addMaster <nick string> <pass string>  : adds a new",
		"- !delMaster <nick string>   : Removes a master from the system",
		"- !removeTopic <topicId int> : Removes a topic",
		"- !say <message string>     : Let the bot say something",
		"- !quit <message string>     : Shutdown the bot and leave a goodbye message",
		"-",
	}

	for _, element := range help {
		bot.sendMessage(receiver, element+"\r\n")
	}
	for _, element := range adminHelp {
		bot.sendMessage(receiver, element+"\r\n")
	}
}

func main() {
	cfgFile, _ := os.Open("bot.json")
	decoder := json.NewDecoder(cfgFile)
	cfg := Configuration{}
	err := decoder.Decode(&cfg)
	if err != nil {
		fmt.Println("ERROR cfg: ", err)
		return
	}

	fmt.Println(cfg)
	bot := new(Bot)
	bot.activeAdmins = make(map[string]bool)
	bot.suggestions = make(map[int]Title)
	bot.writeChan = make(chan *IrcMessage)

	bot.config = &cfg
	bot.conn, err = textproto.Dial("tcp", cfg.Host+":"+cfg.Port)

	if err != nil {
		fmt.Printf("ERROR: %s\n", err)
		return
	}
	fmt.Println(bot)
	bot.startBot()
}
