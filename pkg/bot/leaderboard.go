package bot

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/dustin/go-humanize"
	"github.com/lunemec/eve-quartermaster/pkg/repository"
	"github.com/pkg/errors"
)

// leaderboard will be called every time a new
// message is created on any channel that the autenticated bot has access to.
func (b *quartermasterBot) leaderboard(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore all messages created by the bot itself.
	if m.Author.ID == s.State.User.ID {
		return
	}

	// Force reloading of price from API.
	if strings.HasPrefix(m.Content, "!leaderboard") {
		paramsStr := strings.TrimPrefix(m.Content, "!leaderboard")
		paramsStr = strings.TrimSpace(paramsStr)
		params := strings.Split(paramsStr, " ")

		b.log.Infow("Responding to !leaderboard", "channel_id", m.ChannelID, "msg", m.Content, "params", params)

		var (
			err                error
			dateStart, dateEnd time.Time
		)
		if len(params) == 2 {
			format := "2006-01-02"
			dateStart, err = time.Parse(format, params[0])
			if err != nil {
				b.log.Errorw("error parsing dateStart", "error", err, "start_date", dateStart, "end_date", dateEnd)

				b.sendError(errors.Wrap(err, "unknown date format, use YYYY-MM-DD"), m.ChannelID)
				return
			}
			dateEnd, err = time.Parse(format, params[1])
			if err != nil {
				b.log.Errorw("error parsing dateEnd", "error", err, "start_date", dateStart, "end_date", dateEnd)

				b.sendError(errors.Wrap(err, "unknown date format, use YYYY-MM-DD"), m.ChannelID)
				return
			}
		} else {
			now := time.Now()
			currentYear, currentMonth, _ := now.Date()

			dateStart = time.Date(currentYear, currentMonth, 1, 0, 0, 0, 0, time.UTC)
			dateEnd = dateStart.AddDate(0, 1, -1)
		}

		priceData, err := b.repository.SeekPrices(dateStart, dateEnd)
		if err != nil {
			b.log.Errorw("error seeking price history", "error", err, "start_date", dateStart, "end_date", dateEnd)

			b.sendError(err, m.ChannelID)
			return
		}

		message := b.leaderboardMessage(priceData, dateStart, dateEnd)
		_, err = b.discord.ChannelMessageSendEmbed(m.ChannelID, message)
		if err != nil {
			b.log.Errorw("error sending message for !leaderboard", "error", err)

			b.sendError(err, m.ChannelID)
			return
		}
	}
}

type haulingStats struct {
	Contracts  int
	TotalPrice uint64
	IssuerID   int32
}

func (b *quartermasterBot) leaderboardMessage(priceData []repository.PriceData, dateStart, dateEnd time.Time) *discordgo.MessageEmbed {
	statsPerIssuer := make(map[int32]haulingStats)
	for _, priceDatum := range priceData {
		// Issuers with ID 0 are items that were !price set, or !migrate'd.
		if priceDatum.IssuerID == 0 {
			continue
		}
		stats := statsPerIssuer[priceDatum.IssuerID]
		stats.IssuerID = priceDatum.IssuerID
		stats.Contracts++
		stats.TotalPrice += priceDatum.Price
		statsPerIssuer[priceDatum.IssuerID] = stats
	}
	// Now collect into slice and order by number of contracts.
	var stats []haulingStats
	for _, stat := range statsPerIssuer {
		stats = append(stats, stat)
	}
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Contracts > stats[j].Contracts
	})
	// We only want top 10 on the leaderboard.
	if len(stats) >= 10 {
		stats = stats[:10]
	}

	var msgParts []string
	for i, stat := range stats {
		position := i + 1
		extraIcon := ""
		if position == 1 {
			extraIcon = ":tada: "
		}

		msg := fmt.Sprintf("%s**%s** `%s` with **%d contracts** worth **%d M ISK**",
			extraIcon,
			humanize.Ordinal(position),
			b.idToName(stat.IssuerID),
			stat.Contracts,
			stat.TotalPrice/1000000,
		)
		msgParts = append(msgParts, msg)
	}

	currentYear, currentMonth, _ := dateStart.Date()
	title := fmt.Sprintf(":crown: Leaderboard for %s %d", currentMonth.String(), currentYear)

	if dateStart.Month() != dateEnd.Month() {
		startYear, startMonth, _ := dateStart.Date()
		endYear, endMonth, _ := dateEnd.Date()
		title = fmt.Sprintf(":crown: Leaderboard for %s %d - %s %d", startMonth.String(), startYear, endMonth.String(), endYear)
	}

	return &discordgo.MessageEmbed{
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: "https://i.imgur.com/ZwUn8DI.jpg",
		},
		Color:       0x00ff00,
		Description: strings.Join(msgParts, "\n"),
		Timestamp:   time.Now().Format(time.RFC3339), // Discord wants ISO8601; RFC3339 is an extension of ISO8601 and should be completely compatible.
		Title:       title,
	}
}
