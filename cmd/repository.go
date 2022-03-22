package cmd

import (
	"fmt"

	"github.com/lunemec/eve-quartermaster/pkg/repository"

	"github.com/k0kubun/pp/v3"
	"github.com/spf13/cobra"
)

// repositoryCmd is top level command for repository manipulation.
var repositoryCmd = &cobra.Command{
	Use:   "repository",
	Short: "Repository manipulation functions",
}

// migrateCmd is command to migrate JSON -> bbolt DB.
var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate JSON repository to new bbolt DB",
	Run:   migrateRepository,
}

// readCmd is command to migrate JSON -> bbolt DB.
var readCmd = &cobra.Command{
	Use:   "read",
	Short: "Read contents of bbolt DB for debugging",
}

// readDoctrinesCmd is command to migrate JSON -> bbolt DB.
var readDoctrinesCmd = &cobra.Command{
	Use:   "doctrines",
	Short: "Print doctrines from repository",
	Run:   readDoctrines,
}

// readPriceHistoryCmd is command to migrate JSON -> bbolt DB.
var readPriceHistoryCmd = &cobra.Command{
	Use:   "price_history",
	Short: "Print price history from repository",
	Run:   readPriceHistory,
}

var (
	jsonRepositoryFile  string
	bboltRepositoryFile string
)

func init() {
	rootCmd.AddCommand(repositoryCmd)
	repositoryCmd.AddCommand(migrateCmd)
	repositoryCmd.AddCommand(readCmd)
	readCmd.AddCommand(readDoctrinesCmd)
	readCmd.AddCommand(readPriceHistoryCmd)

	migrateCmd.Flags().StringVar(&jsonRepositoryFile, "json_repository_file", "repository.json", "path to JSON repository json to save doctrine data (default repository.json)")
	migrateCmd.Flags().StringVar(&bboltRepositoryFile, "bbolt_repository_file", "repository.db", "path to bbolt repository json to save doctrine data (default repository.db)")

	readCmd.Flags().StringVar(&bboltRepositoryFile, "repository_file", "repository.db", "path to bbolt repository json to save doctrine data (default repository.db)")
}

func migrateRepository(cmd *cobra.Command, args []string) {
	jsonRepository, err := repository.NewJSONRepository(jsonRepositoryFile)
	if err != nil {
		panic(fmt.Sprintf("error inicializing json repository file: %+v", err))
	}
	bboltRepository, err := repository.NewBBoltRepository(bboltRepositoryFile)
	if err != nil {
		panic(fmt.Sprintf("error inicializing bbolt repository file: %+v", err))
	}
	defer func() {
		err := bboltRepository.Close()
		if err != nil {
			fmt.Printf("ERROR closing DB: %+v\n", err)
		}
	}()

	doctrines, err := jsonRepository.ReadAll()
	if err != nil {
		panic(err)
	}
	err = bboltRepository.WriteAll(doctrines)
	if err != nil {
		panic(err)
	}
	bboltDoctrines, err := bboltRepository.ReadAll()
	if err != nil {
		panic(err)
	}
	fmt.Printf("WROTE %d doctrines.\n", len(bboltDoctrines))
	fmt.Printf("Repositories equal? %t\n", len(doctrines) == len(bboltDoctrines))
}

func readDoctrines(cmd *cobra.Command, args []string) {
	bboltRepository, err := repository.NewBBoltRepository(bboltRepositoryFile)
	if err != nil {
		panic(fmt.Sprintf("error inicializing bbolt repository file: %+v", err))
	}
	defer func() {
		err := bboltRepository.Close()
		if err != nil {
			fmt.Printf("ERROR closing DB: %+v\n", err)
		}
	}()

	bboltDoctrines, err := bboltRepository.ReadAll()
	if err != nil {
		panic(err)
	}

	for _, doctrine := range bboltDoctrines {
		pp.Println(doctrine)
	}
}

func readPriceHistory(cmd *cobra.Command, args []string) {
	bboltRepository, err := repository.NewBBoltRepository(bboltRepositoryFile)
	if err != nil {
		panic(fmt.Sprintf("error inicializing bbolt repository file: %+v", err))
	}
	defer func() {
		err := bboltRepository.Close()
		if err != nil {
			fmt.Printf("ERROR closing DB: %+v\n", err)
		}
	}()

	bboltDoctrines, err := bboltRepository.Prices()
	if err != nil {
		panic(err)
	}

	for _, doctrine := range bboltDoctrines {
		pp.Println(doctrine)
	}
}
