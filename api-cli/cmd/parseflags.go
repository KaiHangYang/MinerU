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
	fl.StringVar(&f.lang, "lang", "ch", "OCR language for cloud/legacy servers; V1 uses the server --language setting")
	fl.StringVar(&f.model, "model", "", `local V1 tier: flash/basic/standard/advanced (default standard); legacy model: pipeline/vlm/hybrid-engine; cloud default: pipeline`)
	fl.BoolVar(&f.ocr, "ocr", false, "force OCR parsing (for image-based/scanned PDFs)")
	fl.BoolVar(&f.noFormula, "no-formula", false, "disable formula recognition (cloud/legacy only; V1 always enables it)")
	fl.BoolVar(&f.noTable, "no-table", false, "disable table recognition (cloud/legacy only; V1 always enables it)")
	fl.StringVar(&f.pageRange, "pages", "", `page range to parse, e.g. "1-10" (cloud or local V1)`)
	fl.BoolVar(&f.verbose, "verbose", false, "keep all available JSON and other artifacts instead of just markdown + images; legacy local servers require this at submit time")
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
