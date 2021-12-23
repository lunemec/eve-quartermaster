package discord

import (
	"time"

	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"
)

// helpHandler will be called every time a new
// message is created on any channel that the autenticated bot has access to.
func (h *discordHandler) helpHandler(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore all messages created by the bot itself.
	if m.Author.ID == s.State.User.ID {
		return
	}

	if m.Content != "!help" && m.Content != "!quartermaster" {
		return
	}
	msg := "I'll keep you updated about our current doctrine ship stock listed on contracts. \n\n" +
		"Here is the list of commands you can use:\n" +
		"`!help` or `!quartermaster` - shows this help message\n" +
		"`!report` or `!qm` - shows a report of missing stock\n" +
		"`!report full` - shows full report of required doctrines with stock/missing counts\n" +
		"`!stock` - shows currently available ships on contract\n" +
		"`!require NN Alliance|Corporation Doctrine name` - require to have `Doctrine name` `NN`" +
		" times on alliance or corporation contracts at all times (0 to remove)\n" +
		"`!require list` - list of doctrine ships required to have on contract at all times\n" +
		"`!parse excel` - parse copy+pasted columns from excel (sheet)"

	_, err := h.discord.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
		Title: "Hello, I'm your Quartermaster.",
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: "https://i.imgur.com/ZwUn8DI.jpg",
		},
		Color:       0x00ff00,
		Description: msg,
		Timestamp:   time.Now().Format(time.RFC3339), // Discord wants ISO8601; RFC3339 is an extension of ISO8601 and should be completely compatible.
	})
	if err != nil {
		if err != nil {
			h.log.Error("error sending message for !help", zap.Error(err))
			return
		}
	}
}
