package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/nlook-service/nlook-router/internal/config"
	"github.com/nlook-service/nlook-router/internal/db"
	"github.com/nlook-service/nlook-router/internal/eval"
	"github.com/nlook-service/nlook-router/internal/llm"
	"github.com/spf13/cobra"
)

var evalCmd = &cobra.Command{
	Use:   "eval",
	Short: "Evaluation framework for AI accuracy testing",
}

// --- eval create ---

var evalCreateType string

var evalCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new eval set",
	Args:  cobra.ExactArgs(1),
	RunE:  runEvalCreate,
}

// --- eval add ---

var evalAddInput string
var evalAddExpected string

var evalAddCmd = &cobra.Command{
	Use:   "add <set-id>",
	Short: "Add a case to an eval set",
	Args:  cobra.ExactArgs(1),
	RunE:  runEvalAdd,
}

// --- eval import ---

var evalImportCmd = &cobra.Command{
	Use:   "import <set-id> <file.json>",
	Short: "Import cases from a JSON file",
	Args:  cobra.ExactArgs(2),
	RunE:  runEvalImport,
}

// --- eval list ---

var evalListSetID string

var evalListCmd = &cobra.Command{
	Use:   "list",
	Short: "List eval sets or cases within a set",
	RunE:  runEvalList,
}

// --- eval run ---

var evalRunModel string
var evalRunEvaluator string
var evalRunIterations int

var evalRunCmd = &cobra.Command{
	Use:   "run <set-id>",
	Short: "Run evaluation on an eval set",
	Args:  cobra.ExactArgs(1),
	RunE:  runEvalRun,
}

// --- eval results ---

var evalResultsCmd = &cobra.Command{
	Use:   "results <run-id>",
	Short: "Show results for an eval run",
	Args:  cobra.ExactArgs(1),
	RunE:  runEvalResults,
}

// --- eval delete ---

var evalDeleteCmd = &cobra.Command{
	Use:   "delete <set-id>",
	Short: "Delete an eval set and all its cases",
	Args:  cobra.ExactArgs(1),
	RunE:  runEvalDelete,
}

func init() {
	evalCreateCmd.Flags().StringVar(&evalCreateType, "type", "chat", "eval target type: chat, workflow, skill")

	evalAddCmd.Flags().StringVar(&evalAddInput, "input", "", "input prompt (required)")
	evalAddCmd.Flags().StringVar(&evalAddExpected, "expected", "", "expected output (required)")
	_ = evalAddCmd.MarkFlagRequired("input")
	_ = evalAddCmd.MarkFlagRequired("expected")

	evalListCmd.Flags().StringVar(&evalListSetID, "set", "", "list cases for a specific eval set")

	evalRunCmd.Flags().StringVar(&evalRunModel, "model", "", "target model to evaluate")
	evalRunCmd.Flags().StringVar(&evalRunEvaluator, "evaluator", "", "evaluator model for scoring")
	evalRunCmd.Flags().IntVar(&evalRunIterations, "iterations", 1, "number of iterations per case")

	evalCmd.AddCommand(evalCreateCmd, evalAddCmd, evalImportCmd, evalListCmd, evalRunCmd, evalResultsCmd, evalDeleteCmd)
	rootCmd.AddCommand(evalCmd)
}

// openEvalDB loads config and opens the database.
func openEvalDB() (db.DB, error) {
	cfg, err := config.Load(GetConfigPath())
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	dataDir := config.ConfigDir()
	if cfg.DB.DataDir != "" {
		dataDir = cfg.DB.DataDir
	}
	driver := cfg.DB.Driver
	if driver == "" {
		driver = "sqlite"
	}
	storage, err := db.New(driver, dataDir)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	return storage, nil
}

func runEvalCreate(cmd *cobra.Command, args []string) error {
	storage, err := openEvalDB()
	if err != nil {
		return err
	}
	defer storage.Close()

	now := time.Now()
	set := &eval.EvalSet{
		ID:         uuid.New().String(),
		Name:       args[0],
		TargetType: evalCreateType,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := storage.UpsertEvalSet(cmd.Context(), set); err != nil {
		return fmt.Errorf("create eval set: %w", err)
	}

	if JSONOutput {
		return PrintJSON(set)
	}
	fmt.Printf("Created eval set: %s (%s)\n", set.ID, set.Name)
	return nil
}

func runEvalAdd(cmd *cobra.Command, args []string) error {
	storage, err := openEvalDB()
	if err != nil {
		return err
	}
	defer storage.Close()

	c := &eval.EvalCase{
		ID:             uuid.New().String(),
		EvalSetID:      args[0],
		Input:          evalAddInput,
		ExpectedOutput: evalAddExpected,
		CreatedAt:      time.Now(),
	}
	if err := storage.InsertEvalCase(cmd.Context(), c); err != nil {
		return fmt.Errorf("add eval case: %w", err)
	}

	if JSONOutput {
		return PrintJSON(c)
	}
	fmt.Printf("Added case: %s\n", c.ID)
	return nil
}

func runEvalImport(cmd *cobra.Command, args []string) error {
	storage, err := openEvalDB()
	if err != nil {
		return err
	}
	defer storage.Close()

	setID := args[0]
	filePath := args[1]

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	var cases []struct {
		Input          string `json:"input"`
		ExpectedOutput string `json:"expected_output"`
		Context        string `json:"context,omitempty"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		return fmt.Errorf("parse JSON: %w", err)
	}

	ctx := cmd.Context()
	for _, c := range cases {
		ec := &eval.EvalCase{
			ID:             uuid.New().String(),
			EvalSetID:      setID,
			Input:          c.Input,
			ExpectedOutput: c.ExpectedOutput,
			Context:        c.Context,
			CreatedAt:      time.Now(),
		}
		if err := storage.InsertEvalCase(ctx, ec); err != nil {
			return fmt.Errorf("insert case: %w", err)
		}
	}

	fmt.Printf("Imported %d cases into set %s\n", len(cases), setID)
	return nil
}

func runEvalList(cmd *cobra.Command, args []string) error {
	storage, err := openEvalDB()
	if err != nil {
		return err
	}
	defer storage.Close()

	ctx := cmd.Context()

	if evalListSetID != "" {
		// List cases for a specific set
		cases, err := storage.ListEvalCases(ctx, evalListSetID)
		if err != nil {
			return fmt.Errorf("list cases: %w", err)
		}
		if JSONOutput {
			return PrintJSON(cases)
		}
		headers := []string{"ID", "INPUT", "EXPECTED"}
		rows := make([][]string, 0, len(cases))
		for _, c := range cases {
			input := c.Input
			if len(input) > 60 {
				input = input[:60] + "..."
			}
			expected := c.ExpectedOutput
			if len(expected) > 60 {
				expected = expected[:60] + "..."
			}
			rows = append(rows, []string{c.ID[:8], input, expected})
		}
		return PrintTable(headers, rows, cases)
	}

	// List all eval sets
	sets, err := storage.ListEvalSets(ctx)
	if err != nil {
		return fmt.Errorf("list eval sets: %w", err)
	}
	if JSONOutput {
		return PrintJSON(sets)
	}
	headers := []string{"ID", "NAME", "TYPE", "CREATED"}
	rows := make([][]string, 0, len(sets))
	for _, s := range sets {
		rows = append(rows, []string{s.ID[:8], s.Name, s.TargetType, s.CreatedAt.Format(time.RFC3339)})
	}
	return PrintTable(headers, rows, sets)
}

func runEvalRun(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(GetConfigPath())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg.ApplyLLMEnv()

	storage, err := openEvalDB()
	if err != nil {
		return err
	}
	defer storage.Close()

	engine := llm.NewEngine()

	evaluatorModel := evalRunEvaluator
	if evaluatorModel == "" {
		evaluatorModel = cfg.Eval.EvaluatorModel
	}
	if evaluatorModel == "" {
		evaluatorModel = engine.Model()
	}

	iterations := evalRunIterations
	if iterations < 1 {
		iterations = cfg.Eval.DefaultIterations
	}
	if iterations < 1 {
		iterations = 1
	}
	if cfg.Eval.MaxIterations > 0 && iterations > cfg.Eval.MaxIterations {
		iterations = cfg.Eval.MaxIterations
	}

	runner := eval.NewEvalRunner(storage, engine, evaluatorModel)

	ctx := cmd.Context()
	run, err := runner.Run(ctx, args[0], eval.RunOptions{
		EvaluatorModel: evaluatorModel,
		TargetModel:    evalRunModel,
		NumIterations:  iterations,
	})
	if err != nil {
		return fmt.Errorf("eval run: %w", err)
	}

	if JSONOutput {
		return PrintJSON(run)
	}

	fmt.Println()
	fmt.Printf("  Run:       %s\n", run.ID[:8])
	fmt.Printf("  Status:    %s\n", run.Status)
	fmt.Printf("  Target:    %s\n", run.TargetModel)
	fmt.Printf("  Evaluator: %s\n", run.EvaluatorModel)
	fmt.Printf("  Cases:     %d/%d completed\n", run.CompletedCases, run.TotalCases)
	fmt.Printf("  Avg Score: %.2f / 10\n", run.AvgScore)
	fmt.Printf("  Std Dev:   %.2f\n", run.StdDev)
	fmt.Printf("  Duration:  %s\n", run.CompletedAt.Sub(run.StartedAt).Round(time.Second))
	fmt.Println()
	fmt.Printf("  View details: nlook-router eval results %s\n", run.ID)
	fmt.Println()

	return nil
}

func runEvalResults(cmd *cobra.Command, args []string) error {
	storage, err := openEvalDB()
	if err != nil {
		return err
	}
	defer storage.Close()

	ctx := cmd.Context()
	runID := args[0]

	run, err := storage.GetEvalRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("get eval run: %w", err)
	}
	if run == nil {
		return fmt.Errorf("eval run %q not found", runID)
	}

	results, err := storage.ListEvalResults(ctx, runID)
	if err != nil {
		return fmt.Errorf("list results: %w", err)
	}

	if JSONOutput {
		return PrintJSON(map[string]interface{}{
			"run":     run,
			"results": results,
		})
	}

	fmt.Println()
	fmt.Printf("  Run %s  |  %s  |  avg=%.2f  std=%.2f  |  %d/%d cases\n",
		run.ID[:8], run.Status, run.AvgScore, run.StdDev, run.CompletedCases, run.TotalCases)
	fmt.Println()

	headers := []string{"CASE", "ITER", "SCORE", "LATENCY", "TOKENS", "REASON"}
	rows := make([][]string, 0, len(results))
	for _, r := range results {
		caseID := r.EvalCaseID
		if len(caseID) > 8 {
			caseID = caseID[:8]
		}
		rows = append(rows, []string{
			caseID,
			strconv.Itoa(r.Iteration),
			strconv.Itoa(r.AccuracyScore),
			fmt.Sprintf("%dms", r.LatencyMs),
			fmt.Sprintf("%d/%d", r.TokensIn, r.TokensOut),
			truncateStr(r.AccuracyReason, 50),
		})
	}
	return PrintTable(headers, rows, results)
}

func runEvalDelete(cmd *cobra.Command, args []string) error {
	storage, err := openEvalDB()
	if err != nil {
		return err
	}
	defer storage.Close()

	setID := args[0]
	set, err := storage.GetEvalSet(cmd.Context(), setID)
	if err != nil {
		return fmt.Errorf("get eval set: %w", err)
	}
	if set == nil {
		return fmt.Errorf("eval set %q not found", setID)
	}

	if err := storage.DeleteEvalSet(cmd.Context(), setID); err != nil {
		return fmt.Errorf("delete eval set: %w", err)
	}

	fmt.Printf("Deleted eval set: %s (%s)\n", set.ID, set.Name)
	return nil
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
