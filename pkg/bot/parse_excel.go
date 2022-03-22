package bot

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/lunemec/eve-quartermaster/pkg/repository"
)

var parseExcelRegex = regexp.MustCompile(`(?P<name>.+)\s{4}(?P<number>[0-9]+)\s{4}(?P<contract>[Aa]lliance|[Cc]orporation|[Cc]orp)`)

// parseExcelHandler will be called every time a new
// message is created on any channel that the autenticated bot has access to.
func (b *quartermasterBot) parseExcelHandler(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore all messages created by the bot itself.
	if m.Author.ID == s.State.User.ID {
		return
	}

	if strings.HasPrefix(m.Content, "!parse excel") {
		// Format is (strips ```):
		// !parse excel
		// Doctrine name	1
		// Doctrine 2	10
		commandContent := strings.TrimPrefix(m.Content, "!parse excel")
		commandContent = strings.ReplaceAll(commandContent, "```", "")

		doctrines := parseExcel(commandContent)
		if len(doctrines) == 0 {
			_, err := b.discord.ChannelMessageSend(m.ChannelID, "You are trying to import 0 doctrines, are you sure?")
			if err != nil {
				b.log.Errorw("error sending message for no doctrines from bulk import", "error", err)
				return
			}
			return
		}
		err := b.repository.WriteAll(doctrines)
		if err != nil {
			b.log.Errorw("error saving bulk insert in stock doctrine", "error", err)

			err = b.discord.MessageReactionAdd(m.ChannelID, m.ID, `❌`)
			if err != nil {
				b.log.Errorw("error reacting with :x:", "error", err)
			}
			b.sendError(err, m)
			return
		}
		err = b.discord.MessageReactionAdd(m.ChannelID, m.ID, `👍`)
		if err != nil {
			b.log.Errorw("error reacting with :+1:", "error", err)
		}
	}
}

func parseExcel(input string) []repository.Doctrine {
	var out []repository.Doctrine
	matches := parseExcelRegex.FindAllStringSubmatch(input, -1)
	for _, match := range matches {
		if len(match) != 4 {
			continue
		}
		name := match[1]
		number := match[2]
		contract := strings.ToLower(match[3])
		// impossible to fail since regex matches numbers.
		num, _ := strconv.Atoi(number)
		if num == 0 {
			continue
		}
		out = append(out, repository.Doctrine{
			Name:         name,
			RequireStock: num,
			ContractedOn: repository.ContractedOn(contract),
		})
	}
	return out
}
