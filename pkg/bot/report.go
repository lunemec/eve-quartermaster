package bot

import (
	"github.com/bwmarrin/discordgo"
)

// reportHandler will be called every time a new
// message is created on any channel that the autenticated bot has access to.
func (b *quartermasterBot) reportHandler(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore all messages created by the bot itself.
	if m.Author.ID == s.State.User.ID {
		return
	}

	if m.Content == "!report" {
		missingDoctrines, err := b.reportMissing()
		if err != nil {
			b.log.Errorw("Error checking for missing doctrines",
				"error", err,
			)
			return
		}
		_, err = b.discord.ChannelMessageSendEmbed(b.channelID, b.notifyMessage(missingDoctrines))
		if err != nil {
			b.log.Errorw("error sending report message", "error", err)
		}
		return
	}
}
