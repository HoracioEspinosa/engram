package vault

import "testing"

func TestKindOfMapsExtensionsAndFallsBackToOther(t *testing.T) {
	cases := []struct {
		path string
		want Kind
	}{
		{path: "evidences/01-login/01-pantalla.png", want: KindPNG},
		{path: "01-galeria.JPG", want: KindJPG},
		{path: "01-galeria.jpeg", want: KindJPG},
		{path: "loop.gif", want: KindGIF},
		{path: "shot.webp", want: KindWEBP},
		{path: "diagram.svg", want: KindSVG},
		{path: "flow.mp4", want: KindMP4},
		{path: "flow.webm", want: KindWEBM},
		{path: "respuesta-cruda.json", want: KindJSON},
		{path: "dump.csv", want: KindCSV},
		{path: "lookup-timeout.log", want: KindLOG},
		{path: "notas.txt", want: KindTXT},
		{path: "BASELINE.md", want: KindMD},
		{path: "instrumentation.patch", want: KindPATCH},
		{path: "cambios.diff", want: KindDIFF},
		{path: "reporte.pdf", want: KindPDF},
		{path: "captura.html", want: KindHTML},
		{path: "captura.htm", want: KindHTML},
		{path: "bundle.zip", want: KindZIP},
		{path: "trafico.har", want: KindHAR},
		{path: "run-bench.sh", want: KindOther},
		{path: "Makefile", want: KindOther},
		{path: ".gitkeep", want: KindOther},
		{path: "", want: KindOther},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			if got := KindOf(tc.path); got != tc.want {
				t.Fatalf("KindOf(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestParseCategoryAcceptsTheElevenClosedCategories(t *testing.T) {
	want := []Category{
		CategoryAnalysis, CategoryPlans, CategoryRunbooks, CategoryReports,
		CategoryPatches, CategoryEvidences, CategoryEvidencesQA, CategoryBenchmarks,
		CategoryScripts, CategoryAssets, CategoryExports,
	}
	if got := Categories(); len(got) != len(want) {
		t.Fatalf("expected %d categories, got %d", len(want), len(got))
	}
	for _, c := range want {
		got, err := ParseCategory(string(c))
		if err != nil {
			t.Fatalf("ParseCategory(%q): %v", c, err)
		}
		if got != c {
			t.Fatalf("ParseCategory(%q) = %q", c, got)
		}
	}
	if _, err := ParseCategory("  Evidences-QA  "); err != nil {
		t.Fatalf("ParseCategory must trim and lowercase: %v", err)
	}
	for _, bad := range []string{"logs", "", "misc", "evidencias"} {
		if _, err := ParseCategory(bad); err == nil {
			t.Fatalf("ParseCategory(%q) must fail", bad)
		}
	}
}
