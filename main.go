/**

  Listening live to a podcast recording involves the risk of overhearing a blooper or a funny thing.
  In the past the only way to keep track of those bloopers was to chisel them into stone tablets if you
  want to preserve them for future references.But behold, the mighty Title Bot 3000 is here to help
  mankind to keep track of those precious bloopers.

  This bot has been written with two things in mind. First, I wanted to learn th GO language (golang.org) and
  second I am a listener to the german happyshooting podcast (www.happyshooting.de), a podcast dedicated to all
  things photography. It is - kinda- custom that the live audience can vote for the title of the next release.
  To help with this and to give the audience a convenient way to vote for those title suggestions I came up with
  this bot.

  From a coding point, this bot is crap. Either write your own or use a well established one.
  I am dabbling with it, so please *DO NOT USE THIS CODE*.
  You have been warned!

  That said, if you find an error I am happy if you would let me know and perhaps an explanation why I f*up.

  https://github.com/JSchwehn/GoIrcBot
*/

// code based on http://jarredkenny.com/blog/The-way-to-Go-A-simple-IRC-bot

package main

import (
	"encoding/json"
	"fmt"
	"log"           // todo use the log from go to get rid of the fmt.Print*
	"net/textproto" // Everything to handle string based protocols
	"os"            // needed to interact with files
	"regexp"        // ReExp lib
	"sort"          // maps in go will not return the sequence as we put it in. We have to handle the sorting our self
	"strconv"       // convert from string to other formats like int
	"strings"       // Some string manipulations
	"time"          // needed for the time.Sleep() method.
)

const SLEEP = 500

// milliseconds

// global vars
var (
	Trace *log.Logger // todo use the logger
	Error *log.Logger // todo use the logger
	debug = true      // turn debugging on or off
	/* Preg match to parse common messages we receive. Extracts 4 parts
	- sender
	- host of sender
	- receiver
	- message
	*/
	privMsg = regexp.MustCompile("^:([a-zA-Z0-9`_\\-]+)!.+@(.+)PRIVMSG (#{1,2}[a-zA-Z0-9_]+) :(.*)$")
	/* Preg match to parse direct messages we receive. Extracts 4 parts
	- sender
	- host of sender
	- receiver
	- message
	*/
	directMsg = regexp.MustCompile("^:([a-zA-Z0-9`_\\-]+)!.+@(.+)PRIVMSG ([a-zA-Z0-9_]+) :(.*)$")
	/* Match a PING message, we have to answer with a PONG message. Extract  1 part
	- payload which has to be send back
	*/
	ping = regexp.MustCompile("^PING :([a-zA-Z0-9\\.]+)$")
	/* This reg ex will be used to detect if where successful connected to the IRC Server.
	   Be aware that this can be differ from IRC Network to IRC Network
	*/
	motd = regexp.MustCompile("^:[a-zA-Z\\.]+\\s376(.*)") // end of motd
	/* Sometimes the bot's nick has been taken - a channel split perhaps. In this case the should use a secondary nick
	   and if this fails to, we try to a backtick to the nick until the conflict is resolved.
	   TODO: use the seoondary nick
	*/
	nickCollision = regexp.MustCompile("^:[a-zA-Z\\.]+\\s433(.*)")
	/* This is how the bot matches for commands. Each command has to be prefixed with an ! */
	pCommand = regexp.MustCompile("^!(\\w+)\\s*(.*)")
)

/**
 * Keeps the configuration
 */
type Configuration struct {
	Nick     string   // Nick name for the bot
	RealName string   // Real name for the bot
	Host     string   // irc host to connect to
	Port     string   // and the port to connect to
	Channels []string // List if channels the bot will join after the connect
	Admins   []User   // List of users who are allowed to administer the bot
}

/**
 * This is a single (admin) User
 */
type User struct {
	Name string // the nick is needed to identify a admin
	Host string // to add a bit of protection this will keep a regex to match the host mark
	Pass string // the clear text password of the user
}

/**
 * A Message can be send to a channel or a specified user
 */
type IrcMessage struct {
	sender   string // the user who sent the message
	host     string // host of the sending user
	receiver string // who should received it (channel or an other user)
	message  string // the message .. duh
}

/**
 * The bot can act on commands given to him.
 */
type Command struct {
	sender     string // Who sent the message
	receiver   string // Who should receive the message (channel or an other user)
	answerTo   string // a reply should be sent to this
	command    string // the command
	rawParam   string // everything after the command and the first white space
	rawCommand string // the whole unprocessed command and params
	isPrivate  bool   // if the message has been send as a direct message.
}

/**
 * The bot, keeps track of all admin who are logged in and  tracks title suggestions
 */
type Bot struct {
	activeAdmins map[string]bool  // list of successful logged in users
	voted        map[string]bool  // list of people who had voted
	suggestions  map[int]Title    // list of title suggestions
conn         *textproto.Conn  // the tcp socket
	writeChan    chan *IrcMessage // a go channel used to centralize the writing and to add a flood protection for the bot
	config       *Configuration   // the config for the bot
}

/**
 * A Title contains of the suggestions and some votes for this suggestion
 */
type Title struct {
	titleSuggestion string // The suggestions
	numOfVotes      int    // sum of votes todo: build voting system
}

/////////////// Bot

/**
 * Sends a message to the connection of the bot. It use the go channel "writeChan" to handle the actual writing process
 */
func (bot Bot) sendMessage(receiver, message string) {
	// Throw a pointer into the channel
	bot.writeChan <- &IrcMessage{
		sender:   "",
		receiver: receiver,
		message:  message,
	}
}

/**
 * Main parsing method. This method will handle and parse everything what has been sent on the IRC Network to us.
 * - It will check if we have a nick collision and will handle a conflict
 * - Match for any message not directly sent to the bot (public messages)
 * - Match for any direct message sent to the bot (private messages)
 * - Match any Ping request
 * - Match for the end of the MOTD and hand of to the handler
 */
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

/**
 * Handle nick collisions
 */
func (bot Bot) nickCollisionHandler() {
	if debug {
		fmt.Printf("DEBUG: Nick collision detected (%s). Improvising!\n", bot.config.Nick)
	}
	// Just add a backtick to the nick
	// todo: use the secondary nick first
	bot.config.Nick = bot.config.Nick + "`"
	bot.conn.Cmd("NICK %s\r\n", bot.config.Nick)
}

/**
 * All action we have a confirmed IRC connection. eg. joining the default IRC channels
 */
func (bot Bot) endOfMOTDHandler() {
	// get the default channels from the config
	for _, c := range bot.config.Channels {
		bot.conn.Cmd("JOIN %s\r\n", c)
		if debug {
			fmt.Printf("... joining channel %s \n", c)
		}
		// if we have many channels to join - don't rush it.
		time.Sleep(SLEEP * time.Millisecond)
	}

}

/**
 *  Send a PONG response to a PING request
 */
func (bot Bot) pingHandler(payload string) {
	bot.conn.Cmd("PONG :%s\r\n", payload)
}

/**
 * Transform a direct message to command the bot can handle
 */
func (bot Bot) directMessageHandler(msg IrcMessage) {
	if match := pCommand.FindStringSubmatch(msg.message); match != nil {
		if debug {
			fmt.Printf("Command found: %s from %s\n", msg.message, msg.sender)
		}
		//todo: just send a pointer, no need to copy the whole thing
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

/**
 * Transform a common IRC message to a bot command
 */
func (bot Bot) messageHandler(msg IrcMessage) {
	if match := pCommand.FindStringSubmatch(msg.message); match != nil {
		if debug {
			fmt.Printf("DEBUG: %s", msg)
		}
		//todo: just send a pointer, no need to copy the whole thing
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

/**
 * Giant switch in where the bot handles all commands
 * todo: look into command pattern in go
 * todo: move command into sub modules .so/.dll ?
 */
func (bot Bot) handelCommand(cmd Command) {
	switch cmd.command {
		// display all title suggestions to the channel or to the admin who sent the command as a pm
	case "show":
		if bot.isAdmin(cmd.sender) { // admins only to avoid flood kicks :/
			// sort the output
			keys := make([]int, 0, len(bot.suggestions))
			for k := range bot.suggestions {
				keys = append(keys, k)
			}
			sort.Ints(keys)
			// send them through the sendMessage
			for index := range keys {
				text := fmt.Sprintf("ID: %d Titel: %s (%d)", index, bot.suggestions[index].titleSuggestion, bot.suggestions[index].numOfVotes)
				bot.sendMessage(cmd.answerTo, text)
			}
		}
		// get a title/topic suggestion
	case "title":
		fallthrough
	case "topic":
		bot.handleTitleSuggestion(cmd)
		// authenticate a user
	case "obeyMe":
		if cmd.isPrivate {
			bot.handleLogin(cmd)
		}
		// disconnect, only for admins and as pm
	case "quit":
		if cmd.isPrivate && bot.isAdmin(cmd.sender) {
			bot.conn.Cmd("QUIT %s", cmd.rawParam)
		}
		// logs a admin out
	case "logout":
		if cmd.isPrivate && bot.isAdmin(cmd.sender) {
			delete(bot.activeAdmins, cmd.sender)
		}
		// adds an additional admin, admin only and pm only
	case "addMaster":
		if cmd.isPrivate && bot.isAdmin(cmd.sender) {
			data := strings.Split(cmd.rawParam, " ")
			// this command has two params
			if len(data) != 2 {
				break
			}
			newAdmin := User{
				Name: data[0],
				Pass: data[1],
			}
			// add the new user to the admin slice
			bot.config.Admins = append(bot.config.Admins, newAdmin)
		}
		// enables the bot to join an other channel than the predefined ones, admin and pm only
	case "join":
		if cmd.isPrivate && bot.isAdmin(cmd.sender) {
			bot.conn.Cmd("JOIN %s", cmd.rawParam)
		}
		// leaves a irc channel; admin pm only
	case "leave":
		fallthrough
	case "part":
		if cmd.isPrivate && bot.isAdmin(cmd.sender) {
			data := strings.Split(cmd.rawParam, " ")
			bot.conn.Cmd("PART %s %s", data[0], data[1])
		}
	case "vote":
		bot.handelVote(cmd.sender, cmd.rawParam)
	case "resetVote":
		if bot.isAdmin(cmd.sender) {
			for i, suggestion := range bot.suggestions {
				b := suggestion
				b.numOfVotes = 1
				bot.suggestions[i] = b
			}
			bot.ResetVotes()
		}
	case "resetTitle":
		if bot.isAdmin(cmd.sender) {
			bot.ResetVotes()
			bot.ResetTitles()
		}
	case "del":
		if bot.isAdmin(cmd.sender) {
			pos, err := strconv.Atoi(cmd.rawParam)
			if err != nil {
				fmt.Println(err)
				os.Exit(2)
			}
			//bot.suggestions[pos]
			delete(bot.suggestions, pos)
		}
		// sends a hardcoded message - used for testing
	case "fisch":
		bot.sendMessage(cmd.answerTo, "Fischers Frize hat blaue Brautkleider.")
	}
}

func (bot Bot) ResetTitles() {
	for s := range bot.suggestions {
		delete(bot.suggestions, s)
	}
}

func (bot Bot) ResetVotes() {
	for v := range bot.voted {
		delete(bot.voted, v)
	}
}

func (bot Bot) handelVote(user, id string) {
	if bot.HasVoted(user) {
		return
	}
	pos, err := strconv.Atoi(id)
	if err != nil {
		fmt.Println(err)
		os.Exit(2)
	}
	b := bot.suggestions[pos]
	b.numOfVotes++
	if bot.isAdmin(user) {
		bot.voted[user] = true
	}

	bot.suggestions[pos] = b
	fmt.Print("Votes %s, for %s", bot.suggestions[pos].numOfVotes, bot.suggestions[pos].titleSuggestion)
}

/**
 * Add a suggestion to the suggestions map. There is no duplicate detection yet. It will set the vote to 1
 */
func (bot Bot) handleTitleSuggestion(cmd Command) {
	pos := int(len(bot.suggestions))
	bot.suggestions[pos] = Title{
		numOfVotes:      1,
		titleSuggestion: cmd.rawParam,
	}
	fmt.Printf("%s", bot.suggestions)
}

/**
 * Handles authentications tries from a user.
 * todo: retry lock/slow down, to hamper brute force attacks
 */
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

/**
 * Helper method to check if a message has been sent from admin user
 */
func (bot Bot) isAdmin(nick string) bool {
	if bot.activeAdmins[nick] {
		return true
	}
	return false
}

func (bot Bot) HasVoted(nick string) bool {
	if bot.voted[nick] {
		return true
	}
	return false
}

/**
 * Set the nick for the bot
 */
func (bot Bot) setNick() {
	bot.conn.Cmd("USER %s 8 * :%s\r\n", bot.config.Nick, bot.config.Nick)
	bot.conn.Cmd("NICK %s\r\n", bot.config.Nick)
}

/**
 * A funnel for all writing actions to the irc server. Useful to throttle the output speed and easy logging
 */
func (bot Bot) channelWriter() {
	// read from channel
	msg := <-bot.writeChan
	// todo improve flood protection here
	time.Sleep(SLEEP * time.Millisecond)
	// todo: Log log log
	bot.conn.Cmd("PRIVMSG %s :%s\r\n", msg.receiver, msg.message)
}

func (bot Bot) writeCfg() {

}

/**
 * Sets the Nick after we connected to the server
 * Receives everything the server trows at the bot and it go'ed the string parser and channelWriter
 */
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

func (bot Bot) loadCfg(file string) {

}

func main() {

	// read log file todo: move in function
	cfgFile, _ := os.Open("bot.json")
	decoder := json.NewDecoder(cfgFile)
	cfg := Configuration{}
	err := decoder.Decode(&cfg)
	if err != nil {
		fmt.Println("ERROR cfg: ", err)
		return
	}
	// end config read

	// Build bot
	bot := new(Bot)
	bot.activeAdmins = make(map[string]bool)
	bot.voted = make(map[string]bool)
	bot.suggestions = make(map[int]Title)
	bot.writeChan = make(chan *IrcMessage)
	bot.config = &cfg
	// connect to the server
	bot.conn, err = textproto.Dial("tcp", cfg.Host+":"+cfg.Port)
	if err != nil {
		fmt.Printf("ERROR: %s\n", err)
		return
	}
	bot.startBot()
}
