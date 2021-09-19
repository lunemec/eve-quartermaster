package bot

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/lunemec/eve-quartermaster/pkg/repository"
)

var wantRegex = regexp.MustCompile(`^(?P<number>[0-9]+)\s(?P<name>.*)$`)

// wantHandler will be called every time a new
// message is created on any channel that the autenticated bot has access to.
func (b *quartermasterBot) wantHandler(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore all messages created by the bot itself.
	if m.Author.ID == s.State.User.ID {
		return
	}

	// Want list goes first so that we don't trigger both it and !want.
	if m.Content == "!want list" {
		wantDoctrines, err := b.repository.Read()
		if err != nil {
			b.log.Errorw("error reading want in stock doctrines", "error", err)

			msg := fmt.Sprintf("Sorry, some error happened: %s", err.Error())
			_, err := b.discord.ChannelMessageSend(m.ChannelID, msg)
			if err != nil {
				b.log.Errorw("error responding with error", "error", err)
				return
			}
			return
		}
		_, err = b.discord.ChannelMessageSendEmbed(m.ChannelID, wantListMessage(wantDoctrines))
		if err != nil {
			b.log.Errorw("error sending message for !want list", "error", err)
			return
		}
		return
	}

	if strings.HasPrefix(m.Content, "!want") {
		// Format is: "!want NN Doctrine name", example: "!want 10 Shield Drake"
		commandContent := strings.TrimPrefix(m.Content, "!want ")
		matches := wantRegex.FindAllStringSubmatch(commandContent, -1)
		if len(matches) == 0 || (len(matches) != 0 && len(matches[0]) != 3) {
			// Send back "unrecognised - format is ..."
			msg := fmt.Sprintf("unrecognised !want `%s`, the format is `!want N Some doctrine`", commandContent)
			_, err := b.discord.ChannelMessageSend(m.ChannelID, msg)
			if err != nil {
				b.log.Errorw("error responding to unknown !want", "error", err)
				return
			}
			return
		}

		doctrineName := matches[0][2]
		// It should be impossible to fail since we match with reges [0-9]
		wantInStock, _ := strconv.Atoi(matches[0][1])
		err := b.repository.Set(doctrineName, wantInStock)
		if err != nil {
			b.log.Errorw("error saving want in stock doctrine", "error", err)

			msg := fmt.Sprintf("Sorry, some error happened: %s", err.Error())
			_, err := b.discord.ChannelMessageSend(m.ChannelID, msg)
			if err != nil {
				b.log.Errorw("error responding with error", "error", err)
				return
			}
			return
		}

		msg := fmt.Sprintf("OK, we want to have `%s` `%dx`", doctrineName, wantInStock)
		_, err = b.discord.ChannelMessageSend(m.ChannelID, msg)
		if err != nil {
			b.log.Errorw("error sending response to !want", "error", err)
			return
		}
		return
	}
}

func wantListMessage(wantDoctrines []repository.Doctrine) *discordgo.MessageEmbed {
	var (
		parts []string
	)

	sort.Slice(wantDoctrines, func(i, j int) bool {
		return wantDoctrines[i].Name < wantDoctrines[j].Name
	})

	for _, doctrine := range wantDoctrines {
		parts = append(parts, fmt.Sprintf("%d %s", doctrine.WantInStock, doctrine.Name))
	}

	return &discordgo.MessageEmbed{
		Title: "Want to have on contract",
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: "https://i.imgur.com/pKEZq6F.png",
		},
		Color:       0x00ff00,
		Description: fmt.Sprintf("```\n%s\n```", strings.Join(parts, "\n")),
		Timestamp:   time.Now().Format(time.RFC3339), // Discord wants ISO8601; RFC3339 is an extension of ISO8601 and should be completely compatible.
	}
}
