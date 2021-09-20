package bot

import (
	"time"

	"github.com/bwmarrin/discordgo"
)

// helpHandler will be called every time a new
// message is created on any channel that the autenticated bot has access to.
func (b *quartermasterBot) helpHandler(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore all messages created by the bot itself.
	if m.Author.ID == s.State.User.ID {
		return
	}

	if m.Content != "!help" {
		return
	}
	msg := "I will notify you periodically when doctrine ships" +
		"are missing from contracts compared to what is wanted. \n\n" +
		"Here is the list of commands you can use:\n" +
		"`!help` - shows this help message\n" +
		"`!qm` - shows a report of missing stock\n" +
		"`!stock` - shows currently available ships on contract\n" +
		"`!want NN Doctrine name` - we want to have `Doctrine name` `NN` times on contract (0 to remove)\n" +
		"`!want list` - list of ships we want to have on contract\n" +
		"`!parse excel` - parse copy+pasted columns from excel (sheet)"

	b.discord.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
		Title: "Hello, I am your Quartermaster",
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: "https://i.imgur.com/ZwUn8DI.jpg",
		},
		Color:       0x00ff00,
		Description: msg,
		Timestamp:   time.Now().Format(time.RFC3339), // Discord wants ISO8601; RFC3339 is an extension of ISO8601 and should be completely compatible.
	})
}
