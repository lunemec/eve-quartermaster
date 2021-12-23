package cmd

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	httpHandler "github.com/lunemec/eve-quartermaster/pkg/handlers/http"
	authRepository "github.com/lunemec/eve-quartermaster/pkg/repositories/auth"
	authService "github.com/lunemec/eve-quartermaster/pkg/services/auth"

	"github.com/braintree/manners"
	open "github.com/pbnj/go-open"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// loginCmd represents the login command
var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Login with EVE SSO and save token to be used by the bot",
	Run:   runLogin,
}

func init() {
	rootCmd.AddCommand(loginCmd)
	loginCmd.Flags().StringVarP(&authfile, "auth_file", "a", "auth.bin", "path to file where to save authentication data")
	loginCmd.Flags().StringVarP(&sessionKey, "session_key", "s", "", "session key, use random string")
	loginCmd.Flags().StringVar(&eveClientID, "eve_client_id", "", "EVE APP client id")
	loginCmd.Flags().StringVar(&eveSSOSecret, "eve_sso_secret", "", "EVE APP SSO secret")

	must(loginCmd.MarkFlagRequired("session_key"))
	must(loginCmd.MarkFlagRequired("eve_client_id"))
	must(loginCmd.MarkFlagRequired("eve_sso_secret"))
}

func runLogin(cmd *cobra.Command, args []string) {
	logger, err := zap.NewDevelopment()
	if err != nil {
		panic(fmt.Sprintf("error inicializing logger: %v", err))
	}
	signalChan := make(chan os.Signal, 1)
	// Notify signalChan on SIGINT and SIGTERM.
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	if _, err := os.Stat(authfile); !os.IsNotExist(err) {
		err = os.Remove(authfile)
		if err != nil {
			panic(fmt.Sprintf("unable to delete file: %s please remove it by hand", authfile))
		}
	}
	httpClientInstance := httpClient()

	authRepository := authRepository.NewAuthRepository(authfile)
	authService := authService.NewService(
		logger,
		httpClientInstance,
		authRepository,
		[]byte(sessionKey),
		eveClientID,
		eveSSOSecret,
		eveCallbackURL,
		eveScopes,
	)
	handler := httpHandler.New(
		signalChan,
		logger,
		httpClientInstance,
		authService,
		[]byte(sessionKey),
		eveClientID,
		eveSSOSecret,
		eveCallbackURL,
		eveScopes,
	)
	server := manners.NewWithServer(&http.Server{
		Addr:         addr,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		Handler:      handler,
	})

	go func() {
		for s := range signalChan {
			logger.Info(fmt.Sprintf("Captured %v. Exiting...", s))
			server.Close()
		}
	}()

	// Open default web browser after 1s.
	time.AfterFunc(1*time.Second, func() {
		openAddr := fmt.Sprintf("http://%s", addr)
		logger.Info("Opening browser at address", zap.String("addr", openAddr))
		err := open.Open(openAddr)
		if err != nil {
			logger.Error("Error opening browser", zap.Error(err))
		}
	})

	logger.Info("Listening on address", zap.String("addr", addr))
	if err := server.ListenAndServe(); err != nil {
		logger.Error("ListenAndServe error", zap.Error(err))
	}
}
