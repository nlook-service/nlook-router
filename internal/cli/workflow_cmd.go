package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/nlook-service/nlook-router/internal/apiclient"
	"github.com/nlook-service/nlook-router/internal/config"
)

var workflowCmd = &cobra.Command{
	Use:   "workflow",
	Short: "workflow commands",
}

var workflowListCmd = &cobra.Command{
	Use:   "list",
	Short: "list workflows",
	RunE:  runWorkflowList,
}

var workflowShowCmd = &cobra.Command{
	Use:   "show [id]",
	Short: "show workflow by ID",
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkflowShow,
}

func init() {
	workflowCmd.AddCommand(workflowListCmd, workflowShowCmd)
	Root().AddCommand(workflowCmd)
}

func loadConfigAndClient() (*config.Config, apiclient.Interface, error) {
	path := GetConfigPath()
	cfg, err := config.Load(path)
	if err != nil {
		return nil, nil, err
	}
	config.ApplyEnv(cfg)
	if cfg.APIKey == "" {
		return nil, nil, fmt.Errorf("API key not set; run: nlook-router config set NLOOK_API_KEY <key>")
	}
	client := apiclient.New(cfg.APIURL, cfg.APIKey)
	return cfg, client, nil
}

func runWorkflowList(cmd *cobra.Command, args []string) error {
	_, client, err := loadConfigAndClient()
	if err != nil {
		return err
	}
	list, err := client.ListWorkflows(cmd.Context())
	if err != nil {
		return err
	}
	if JSONOutput {
		return PrintJSON(list)
	}
	headers := []string{"ID", "Title"}
	rows := make([][]string, 0, len(list))
	for _, w := range list {
		rows = append(rows, []string{strconv.FormatInt(w.ID, 10), w.Title})
	}
	return PrintTable(headers, rows, list)
}

func runWorkflowShow(cmd *cobra.Command, args []string) error {
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid id: %w", err)
	}
	_, client, err := loadConfigAndClient()
	if err != nil {
		return err
	}
	w, err := client.GetWorkflow(cmd.Context(), id)
	if err != nil {
		return err
	}
	return PrintJSON(w)
}
