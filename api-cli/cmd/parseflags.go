package cmd

import (
	"github.com/spf13/cobra"

	"mineru-cli/internal/backend"
)

// parseOptFlags holds the parse-option flag values shared by `submit` and
// `parse`.
type parseOptFlags struct {
	lang      string
	model     string
	ocr       bool
	noFormula bool
	noTable   bool
	pageRange string
	verbose   bool
}

func addParseOptFlags(cmd *cobra.Command, f *parseOptFlags) {
	fl := cmd.Flags()
	fl.StringVar(&f.lang, "lang", "ch", "document language, improves OCR accuracy (e.g. ch, en, japan, korean)")
	fl.StringVar(&f.model, "model", "", `parsing model: "pipeline" (fast, general) or "vlm" (higher accuracy; local backend also accepts hybrid-engine etc.); defaults to "hybrid-engine" on the local backend and "pipeline" on cloud`)
	fl.BoolVar(&f.ocr, "ocr", false, "force OCR parsing (for image-based/scanned PDFs)")
	fl.BoolVar(&f.noFormula, "no-formula", false, "disable formula recognition")
	fl.BoolVar(&f.noTable, "no-table", false, "disable table recognition")
	fl.StringVar(&f.pageRange, "pages", "", `page range to parse, e.g. "1-10" (cloud backend only)`)
	fl.BoolVar(&f.verbose, "verbose", false, "keep the full raw output (layout/model/content-list JSON, original file) instead of just markdown + images; local backend must be told this at submit time")
}

func (f parseOptFlags) toParseOptions() backend.ParseOptions {
	return backend.ParseOptions{
		Lang:          f.lang,
		Model:         f.model,
		OCR:           f.ocr,
		FormulaEnable: !f.noFormula,
		TableEnable:   !f.noTable,
		PageRange:     f.pageRange,
		Verbose:       f.verbose,
	}
}
