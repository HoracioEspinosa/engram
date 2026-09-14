package vault

import (
	"path/filepath"
	"strings"
)

// Kind is the coarse file type a scanned file is recorded under. It mirrors
// the evidence kinds the store accepts, so a scan result can be persisted
// without a second translation table.
type Kind string

const (
	KindPNG   Kind = "png"
	KindJPG   Kind = "jpg"
	KindGIF   Kind = "gif"
	KindWEBP  Kind = "webp"
	KindSVG   Kind = "svg"
	KindMP4   Kind = "mp4"
	KindWEBM  Kind = "webm"
	KindJSON  Kind = "json"
	KindCSV   Kind = "csv"
	KindLOG   Kind = "log"
	KindTXT   Kind = "txt"
	KindMD    Kind = "md"
	KindPATCH Kind = "patch"
	KindDIFF  Kind = "diff"
	KindPDF   Kind = "pdf"
	KindHTML  Kind = "html"
	KindZIP   Kind = "zip"
	KindHAR   Kind = "har"
	// KindOther covers everything the table below does not name. It is a real
	// answer, not a failure: a shell script is still evidence worth recording.
	KindOther Kind = "other"
)

var kindByExtension = map[string]Kind{
	".png":   KindPNG,
	".jpg":   KindJPG,
	".jpeg":  KindJPG,
	".gif":   KindGIF,
	".webp":  KindWEBP,
	".svg":   KindSVG,
	".mp4":   KindMP4,
	".webm":  KindWEBM,
	".json":  KindJSON,
	".csv":   KindCSV,
	".log":   KindLOG,
	".txt":   KindTXT,
	".md":    KindMD,
	".patch": KindPATCH,
	".diff":  KindDIFF,
	".pdf":   KindPDF,
	".html":  KindHTML,
	".htm":   KindHTML,
	".zip":   KindZIP,
	".har":   KindHAR,
}

// KindOf maps a path to its kind by extension, case-insensitively, falling
// back to KindOther.
func KindOf(path string) Kind {
	ext := strings.ToLower(filepath.Ext(path))
	if k, ok := kindByExtension[ext]; ok {
		return k
	}
	return KindOther
}
