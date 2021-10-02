package bot

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/adrg/strutil"
	"github.com/adrg/strutil/metrics"
	"github.com/lunemec/eve-quartermaster/pkg/repository"
	"github.com/lunemec/eve-quartermaster/pkg/token"

	"github.com/antihax/goesi"
	"github.com/antihax/goesi/esi"
	"github.com/antihax/goesi/optional"
	"github.com/bwmarrin/discordgo"
	"github.com/pkg/errors"
)

// Bot what a bot does.
type Bot interface {
	Bot() error
}

type quartermasterBot struct {
	ctx         context.Context
	tokenSource token.Source
	log         logger
	esi         *goesi.APIClient
	discord     *discordgo.Session
	channelID   string

	corporationID int32
	allianceID    int32

	checkInterval  time.Duration
	notifyInterval time.Duration

	repository repository.Repository

	// mapping of "wanted" doctrine name last notify time
	notified map[string]time.Time
}

type logger interface {
	Infow(string, ...interface{})
	Errorw(string, ...interface{})
}

// NewQuartermasterBot returns new bot instance.
func NewQuartermasterBot(
	log logger,
	client *http.Client,
	tokenSource token.Source,
	discord *discordgo.Session,
	channelID string,
	corporationID, allianceID int32,
	repository repository.Repository,
	checkInterval, notifyInterval time.Duration,
) Bot {
	log.Infow("EVE Quartermaster starting",
		"check_interval", checkInterval,
		"notify_interval", notifyInterval,
	)

	esi := goesi.NewAPIClient(client, "EVE Quartermaster")
	return &quartermasterBot{
		ctx:            context.WithValue(context.Background(), goesi.ContextOAuth2, tokenSource),
		tokenSource:    tokenSource,
		log:            log,
		esi:            esi,
		discord:        discord,
		channelID:      channelID,
		corporationID:  corporationID,
		allianceID:     allianceID,
		checkInterval:  checkInterval,
		notifyInterval: notifyInterval,
		repository:     repository,
		notified:       make(map[string]time.Time),
	}
}

// Bot - you know, do what a bot does.
func (b *quartermasterBot) Bot() error {
	err := b.discord.Open()
	if err != nil {
		return errors.Wrap(err, "unable to connect to discord")
	}
	// Add handler to listen for "!help" messages as help message.
	b.discord.AddHandler(b.helpHandler)
	// Add handler to listen for "!parse excel" messages for bulk insert from excel (or google) sheet.
	b.discord.AddHandler(b.parseExcelHandler)
	// Add handler to listen for "!qm" messages to show missing doctrines on contract.
	b.discord.AddHandler(b.reportHandler)
	// Add handler to listen for "!stock" messages to list currently available doctrines in stock.
	b.discord.AddHandler(b.stockHandler)
	// Add handler to listen for "!want" messages to manage wanted doctrine numbers to be stocked.
	b.discord.AddHandler(b.wantHandler)

	return b.runForever()
}

type doctrineReport struct {
	doctrine    repository.Doctrine
	haveInStock int
}

func (b *quartermasterBot) runForever() error {
	for {
		var (
			shouldNotifyDoctrine bool
			allDoctrines         []doctrineReport
			notifyDoctrines      = make(map[string]struct{})
		)

		missingCorpDoctrines, missingAllianceDoctrines, err := b.reportMissing()
		if err != nil {
			b.log.Errorw("Error checking for missing doctrines",
				"error", err,
			)
			goto SLEEP
		}

		allDoctrines = append(missingCorpDoctrines, missingAllianceDoctrines...)

		// If just one of the missing doctrines should be notified about, notify about all.
		for _, missingDoctrine := range allDoctrines {
			shouldNotifyDoctrine = b.shouldNotify(missingDoctrine.doctrine.Name)
			if shouldNotifyDoctrine {
				notifyDoctrines[missingDoctrine.doctrine.Name] = struct{}{}
			}
		}

		if len(notifyDoctrines) != 0 {
			_, err = b.discord.ChannelMessageSendEmbed(
				b.channelID,
				b.notifyMessage(
					filterNotifyDoctrines(notifyDoctrines, missingCorpDoctrines),
					filterNotifyDoctrines(notifyDoctrines, missingAllianceDoctrines),
				),
			)
			switch {
			case err != nil:
				b.log.Errorw("Error sending discord message",
					"error", err,
				)
				// In case of error, we fall through to the time.Sleep
				// block. We also do not set the structure as notified
				// and it get picked up on next iteration.
				goto SLEEP
			case err == nil:
				for notifiedDoctrine := range notifyDoctrines {
					b.setWasNotified(notifiedDoctrine)
				}
			}
		}
	SLEEP:
		time.Sleep(b.checkInterval)
	}
}

func (b *quartermasterBot) reportMissing() ([]doctrineReport, []doctrineReport, error) {
	corpContracts, allianceContracts, err := b.loadContracts()
	if err != nil {
		return nil, nil, errors.Wrap(err, "unable to load contracts")
	}

	gotCorpDoctrines := doctrinesAvailable(corpContracts)
	gotAllianceDoctrines := doctrinesAvailable(allianceContracts)
	wantAllDoctrines, err := b.repository.Read()
	if err != nil {
		return nil, nil, errors.Wrap(err, "error reading wanted doctrines")
	}

	wantCorporationDoctrines := filterDoctrines(wantAllDoctrines, repository.Corporation)
	wantAllianceDoctrines := filterDoctrines(wantAllDoctrines, repository.Alliance)

	return b.missingDoctrines(wantCorporationDoctrines, gotCorpDoctrines),
		b.missingDoctrines(wantAllianceDoctrines, gotAllianceDoctrines),
		nil
}

func filterDoctrines(doctrines []repository.Doctrine, contractedOn repository.ContractedOn) []repository.Doctrine {
	var out []repository.Doctrine
	for _, doctrine := range doctrines {
		if doctrine.ContractedOn == contractedOn {
			out = append(out, doctrine)
		}
	}
	return out
}

func (b *quartermasterBot) missingDoctrines(
	wantDoctrines []repository.Doctrine,
	gotDoctrines map[string]int,
) []doctrineReport {
	var missing []doctrineReport
	for _, wantDoctrine := range wantDoctrines {
		if wantDoctrine.WantInStock == 0 {
			continue
		}
		var found bool
		for doctrine, haveInStock := range gotDoctrines {
			namesEqual := compareDoctrineNames(wantDoctrine.Name, doctrine)
			if namesEqual {
				found = true
			}
			if namesEqual && haveInStock < wantDoctrine.WantInStock {
				missing = append(missing, doctrineReport{
					doctrine:    wantDoctrine,
					haveInStock: haveInStock,
				})
			}
		}
		if !found {
			missing = append(missing, doctrineReport{
				doctrine:    wantDoctrine,
				haveInStock: 0,
			})
		}
	}
	sort.Slice(missing, func(i, j int) bool {
		return missing[i].doctrine.Name < missing[j].doctrine.Name
	})
	return missing
}

func filterNotifyDoctrines(notifyDoctrines map[string]struct{}, doctrines []doctrineReport) []doctrineReport {
	var out []doctrineReport
	for _, doctrine := range doctrines {
		_, ok := notifyDoctrines[doctrine.doctrine.Name]
		if ok {
			out = append(out, doctrine)
		}
	}
	return out
}

// loadContracts returns contracts from EVE ESI which are assigned to specified
// assigneeID.
func (b *quartermasterBot) loadContracts() (
	[]esi.GetCorporationsCorporationIdContracts200Ok,
	[]esi.GetCorporationsCorporationIdContracts200Ok,
	error,
) {
	var allContracts []esi.GetCorporationsCorporationIdContracts200Ok

	contractsPage, resp, err := b.esi.ESI.ContractsApi.GetCorporationsCorporationIdContracts(b.ctx, b.corporationID, nil)
	if err != nil {
		return nil, nil, errors.Wrap(err, "error calling ESI API")
	}
	allContracts = append(allContracts, contractsPage...)

	pages, err := strconv.Atoi(resp.Header.Get("X-Pages"))
	if err != nil {
		return nil, nil, errors.Wrap(err, "error converting X-Pages to integer")
	}
	// Fetch additional pages if any (starting page above is 1).
	for i := 2; i <= pages; i++ {
		contractsPage, _, err := b.esi.ESI.ContractsApi.GetCorporationsCorporationIdContracts(
			b.ctx,
			b.corporationID,
			&esi.GetCorporationsCorporationIdContractsOpts{
				Page: optional.NewInt32(int32(i)),
			},
		)
		if err != nil {
			return nil, nil, errors.Wrap(err, "error calling ESI API")
		}
		allContracts = append(allContracts, contractsPage...)
	}

	var (
		corpContracts     []esi.GetCorporationsCorporationIdContracts200Ok
		allianceContracts []esi.GetCorporationsCorporationIdContracts200Ok
	)
	for _, contract := range allContracts {
		if contract.Status != "outstanding" {
			continue
		}
		if contract.AssigneeId == b.corporationID {
			corpContracts = append(corpContracts, contract)
		}
		if contract.AssigneeId == b.allianceID {
			allianceContracts = append(allianceContracts, contract)
		}
	}

	return corpContracts, allianceContracts, nil
}

// doctrinesAvailable returns map of doctrine name -> count of contracts
func doctrinesAvailable(contracts []esi.GetCorporationsCorporationIdContracts200Ok) map[string]int {
	var out = make(map[string]int)
	for _, contract := range contracts {
		var isInAvailable bool
		for doctrineAvailable := range out {
			namesEqual := compareDoctrineNames(doctrineAvailable, contract.Title)
			if namesEqual {
				isInAvailable = true
				out[doctrineAvailable]++
			}
		}
		if !isInAvailable {
			out[contract.Title]++
		}
	}

	return out
}

func compareDoctrineNames(want, have string) bool {
	wantParts := strings.Split(want, " ")
	haveParts := strings.Split(have, " ")

	var wantPartsEqual int
	for _, wantPart := range wantParts {
		wantPartLower := strings.ToLower(wantPart)

		for _, havePart := range haveParts {
			havePartLower := strings.ToLower(havePart)
			if wantPartLower == havePartLower {
				wantPartsEqual++
			}
		}
	}

	// Simple check went OK, all wantParts are found in haveParts.
	if wantPartsEqual >= len(wantParts) {
		return true
	}
	// Otherwise try similarity check.
	// This matches more complicated names containing () and so on.
	metric := metrics.NewJaccard()
	metric.CaseSensitive = false
	similarity := strutil.Similarity(want, have, metric)
	return similarity >= 0.8
}

func (b *quartermasterBot) notifyMessage(missingCorporationDoctrines, missingAllianceDoctrines []doctrineReport) *discordgo.MessageEmbed {
	var parts []string

	parts = append(parts, ":exclamation: ***Alliance contracts***")
	for _, missingDoctrine := range missingAllianceDoctrines {
		parts = append(parts, fmt.Sprintf("**%s** is low in stock, got %d but want %d",
			missingDoctrine.doctrine.Name,
			missingDoctrine.haveInStock,
			missingDoctrine.doctrine.WantInStock,
		))
	}

	parts = append(parts, "\n :grey_exclamation: ***Corporation contracts***")
	for _, missingDoctrine := range missingCorporationDoctrines {
		parts = append(parts, fmt.Sprintf("**%s** is low in stock, got %d but want %d",
			missingDoctrine.doctrine.Name,
			missingDoctrine.haveInStock,
			missingDoctrine.doctrine.WantInStock,
		))
	}

	var color = 0xff0000
	return &discordgo.MessageEmbed{
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: "https://i.imgur.com/ZwUn8DI.jpg",
		},
		Color:       color,
		Description: strings.Join(parts, "\n"),
		Timestamp:   time.Now().Format(time.RFC3339), // Discord wants ISO8601; RFC3339 is an extension of ISO8601 and should be completely compatible.
		Title:       "Doctrine ship stock low",
	}
}

// shouldNotify checks if given doctrine should be notified
// right now.
func (b *quartermasterBot) shouldNotify(doctrineName string) bool {
	return !b.wasNotified(doctrineName)
}

// setWasNotified stores information that doctrine was already
// notified at time.Now()
func (b *quartermasterBot) setWasNotified(doctrineName string) {
	b.notified[doctrineName] = time.Now()
}

// wasNotified checks if this doctrine was notified within
// b.notifyInterval.
func (b *quartermasterBot) wasNotified(doctrineName string) bool {
	notifyTime, ok := b.notified[doctrineName]
	if !ok {
		return false
	}
	if time.Since(notifyTime) > b.notifyInterval {
		return false
	}
	return true
}
