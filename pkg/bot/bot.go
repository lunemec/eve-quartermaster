package bot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
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

	// mapping of "requireed" doctrine name last notify time
	notified map[string]time.Time

	// ID -> names map
	names *sync.Map
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
		names:          new(sync.Map),
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
	// Add handler to listen for "!require" messages to manage target doctrine numbers to be stocked.
	b.discord.AddHandler(b.requireHandler)

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
			msg := b.notifyMessage(
				filterNotifyDoctrines(notifyDoctrines, missingCorpDoctrines),
				filterNotifyDoctrines(notifyDoctrines, missingAllianceDoctrines),
			)
			if msg == nil {
				b.log.Infow("No doctrines added yet, sleeping.")
				goto SLEEP
			}
			_, err = b.discord.ChannelMessageSendEmbed(
				b.channelID,
				msg,
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
	allContracts, err := b.loadContracts()
	if err != nil {
		return nil, nil, errors.Wrap(err, "unable to load contracts")
	}

	corporationContracts, allianceContracts := b.filterAndGroupContracts(
		allContracts,
		statusOutstanding,
		typeItemExchange,
		true,
	)
	gotCorporationDoctrines := doctrinesAvailable(corporationContracts)
	gotAllianceDoctrines := doctrinesAvailable(allianceContracts)
	requireAllDoctrines, err := b.repository.Read()
	if err != nil {
		return nil, nil, errors.Wrap(err, "error reading required doctrines")
	}

	requireCorporationDoctrines := filterDoctrines(requireAllDoctrines, repository.Corporation)
	requireAllianceDoctrines := filterDoctrines(requireAllDoctrines, repository.Alliance)

	return b.missingDoctrines(requireCorporationDoctrines, gotCorporationDoctrines),
		b.missingDoctrines(requireAllianceDoctrines, gotAllianceDoctrines),
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
	requireDoctrines []repository.Doctrine,
	gotContracts map[string]int,
) []doctrineReport {
	var (
		report []doctrineReport
	)

	doctrinesDiff := diffDoctrines(requireDoctrines, gotContracts)
	for _, doctrineReport := range doctrinesDiff {
		if doctrineReport.haveInStock < doctrineReport.doctrine.RequireStock {
			report = append(report, doctrineReport)
		}
	}

	sort.Slice(report, func(i, j int) bool {
		return report[i].doctrine.Name < report[j].doctrine.Name
	})
	return report
}

func diffDoctrines(
	requireDoctrines []repository.Doctrine,
	gotContracts map[string]int,
) map[string]doctrineReport {
	var (
		missing = make(map[string]doctrineReport)
	)

	// Iterate over contracts, and decrement from missing map for
	// each found doctrine ship.
	for _, requireDoctrine := range requireDoctrines {
		found := false
		for contractName, haveInStock := range gotContracts {
			namesEqual := compareDoctrineNames(requireDoctrine.Name, contractName)
			if namesEqual {
				found = true

				report := missing[requireDoctrine.Name]
				report.doctrine = requireDoctrine
				report.haveInStock += haveInStock
				missing[requireDoctrine.Name] = report
			}
		}
		if !found {
			missing[requireDoctrine.Name] = doctrineReport{
				doctrine:    requireDoctrine,
				haveInStock: 0,
			}
		}
	}

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
	error,
) {
	var allContracts []esi.GetCorporationsCorporationIdContracts200Ok

	contractsPage, resp, err := b.esi.ESI.ContractsApi.GetCorporationsCorporationIdContracts(b.ctx, b.corporationID, nil)
	if err != nil {
		return nil, errors.Wrap(err, "error calling ESI API")
	}
	allContracts = append(allContracts, contractsPage...)

	pages, err := strconv.Atoi(resp.Header.Get("X-Pages"))
	if err != nil {
		return nil, errors.Wrap(err, "error converting X-Pages to integer")
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
			return nil, errors.Wrap(err, "error calling ESI API")
		}
		allContracts = append(allContracts, contractsPage...)
	}

	return allContracts, nil
}

type contractStatus string
type contractType string

const (
	// Contract Statuses.
	statusOutstanding        contractStatus = "outstanding"
	statusInProgress         contractStatus = "in_progress"
	statusFinishedIssuer     contractStatus = "finished_issuer"
	statusFinishedContractor contractStatus = "finished_contractor"
	statusFinished           contractStatus = "finished"
	statusCancelled          contractStatus = "cancelled"
	statusRejected           contractStatus = "rejected"
	statusFailed             contractStatus = "failed"
	statusDeleted            contractStatus = "deleted"
	statusReversed           contractStatus = "reversed"

	// Contract Types.
	typeUnknown      contractType = "unknown"
	typeItemExchange contractType = "item_exchange"
	typeAuction      contractType = "auction"
	typeCourier      contractType = "courier"
	typeLoadn        contractType = "loan"
)

func (b *quartermasterBot) filterAndGroupContracts(
	contracts []esi.GetCorporationsCorporationIdContracts200Ok,
	status contractStatus,
	contractType contractType,
	skipExpired bool,
) (
	[]esi.GetCorporationsCorporationIdContracts200Ok,
	[]esi.GetCorporationsCorporationIdContracts200Ok,
) {
	var (
		corpContracts     []esi.GetCorporationsCorporationIdContracts200Ok
		allianceContracts []esi.GetCorporationsCorporationIdContracts200Ok
	)
	for _, contract := range contracts {
		if contract.Type_ != string(contractType) {
			continue
		}
		// Skip contract that are different status from what we require.
		if contract.Status != string(status) {
			continue
		}
		// Skip expired contracts.
		if skipExpired && contract.DateExpired.Before(time.Now()) {
			continue
		}
		if contract.AssigneeId == b.corporationID {
			corpContracts = append(corpContracts, contract)
		}
		if contract.AssigneeId == b.allianceID {
			allianceContracts = append(allianceContracts, contract)
		}
	}

	return corpContracts, allianceContracts
}

func (b *quartermasterBot) filterAlertContracts(
	contracts []esi.GetCorporationsCorporationIdContracts200Ok,
) []esi.GetCorporationsCorporationIdContracts200Ok {
	var (
		alertContracts []esi.GetCorporationsCorporationIdContracts200Ok
	)
	for _, contract := range contracts {
		if contract.AssigneeId != b.corporationID && contract.AssigneeId != b.allianceID {
			continue
		}

		switch contract.Status {
		case string(statusCancelled), string(statusDeleted), string(statusFinished), string(statusFinishedContractor), string(statusFinishedIssuer):
			continue
		}
		// Alert expired contract.
		if contract.DateExpired.Before(time.Now()) {
			alertContracts = append(alertContracts, contract)
			continue
		}
		if contract.Type_ != string(typeItemExchange) {
			alertContracts = append(alertContracts, contract)
			continue
		}
	}

	return alertContracts
}

func (b *quartermasterBot) idToName(id int32) string {
	nameInterface, ok := b.names.Load(id)
	if !ok {
		names, resp, err := b.esi.ESI.UniverseApi.PostUniverseNames(b.ctx, []int32{id}, nil)
		if err != nil {
			// Do not cache on error, so it will retry next time.
			// Return back ID.
			body, _ := io.ReadAll(resp.Body)
			defer resp.Body.Close()
			b.log.Errorw("Error translating ID to name", "error", err, "body", body)
			return fmt.Sprint(id)
		}
		// If we do not have names, then return the ID back.
		if len(names) != 1 {
			b.log.Errorw("Error translating ID to name, returned no names", "len", len(names))
			return fmt.Sprint(id)
		}
		name := names[0].Name
		b.names.Store(id, name)
		return name
	}
	return nameInterface.(string)
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

func compareDoctrineNames(require, have string) bool {
	requireParts := strings.Split(require, " ")
	haveParts := strings.Split(have, " ")

	var requirePartsEqual int
	for _, requirePart := range requireParts {
		requirePartLower := strings.ToLower(requirePart)

		for _, havePart := range haveParts {
			havePartLower := strings.ToLower(havePart)
			if requirePartLower == havePartLower {
				requirePartsEqual++
			}
		}
	}

	// Simple check went OK, all requireParts are found in haveParts.
	if requirePartsEqual >= len(requireParts) {
		return true
	}
	// Otherwise try similarity check.
	// This matches more complicated names containing () and so on.
	metric := metrics.NewJaccard()
	metric.CaseSensitive = false
	similarity := strutil.Similarity(require, have, metric)
	return similarity >= 0.8
}

func (b *quartermasterBot) notifyMessage(missingCorporationDoctrines, missingAllianceDoctrines []doctrineReport) *discordgo.MessageEmbed {
	var parts []string

	// Add "Alliance" block only if there is something to show there.
	if len(missingAllianceDoctrines) != 0 {
		parts = append(parts, ":exclamation: ***Alliance contracts***")
		for _, missingDoctrine := range missingAllianceDoctrines {
			parts = append(parts, fmt.Sprintf("**%s** is low in stock, have %d but require %d",
				missingDoctrine.doctrine.Name,
				missingDoctrine.haveInStock,
				missingDoctrine.doctrine.RequireStock,
			))
		}
	}

	// Add "Corporation" block only if there is something to show there.
	if len(missingCorporationDoctrines) != 0 {
		parts = append(parts, "\n :grey_exclamation: ***Corporation contracts***")
		for _, missingDoctrine := range missingCorporationDoctrines {
			parts = append(parts, fmt.Sprintf("**%s** is low in stock, have %d but require %d",
				missingDoctrine.doctrine.Name,
				missingDoctrine.haveInStock,
				missingDoctrine.doctrine.RequireStock,
			))
		}
	}

	if len(parts) == 0 {
		return nil
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

func (b *quartermasterBot) sendError(errIn error, m *discordgo.MessageCreate) {
	msg := fmt.Sprintf("Sorry, some error happened: %s", errIn.Error())
	_, err := b.discord.ChannelMessageSend(m.ChannelID, msg)
	if err != nil {
		b.log.Errorw("error responding with error", "error", err, "original_error", errIn)
	}
}

func (b *quartermasterBot) sendNoDoctrinesAddedMessage(m *discordgo.MessageCreate) {
	msg := "Nothing added yet, use `!require` command to add doctrines, or check `!help` for more information."
	_, err := b.discord.ChannelMessageSend(
		m.ChannelID,
		msg,
	)
	if err != nil {
		b.log.Errorw("error sending help report full message", "error", err)
	}
}
