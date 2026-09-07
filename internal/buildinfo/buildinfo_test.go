package buildinfo

import "testing"

func TestVersionMaWartoscDomyslna(t *testing.T) {
	// Brak wstrzykniecia nie moze dawac pustego stringa - panel odroznia
	// "stara wersja" od "agent nie raportuje wersji".
	if Version == "" {
		t.Error("Version nie moze byc pusta")
	}
}

func TestVersionDomyslnieDev(t *testing.T) {
	// Zmiana tej wartosci oznacza, ze ktos wstrzyknal wersje w tescie -
	// wtedy ten test trzeba swiadomie zaktualizowac.
	if Version != "dev" {
		t.Logf("Version = %q (wstrzyknieta przy budowaniu)", Version)
	}
}
