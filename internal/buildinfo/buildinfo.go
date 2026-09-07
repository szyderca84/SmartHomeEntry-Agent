// Package buildinfo niesie wersje agenta wstrzykiwana przy budowaniu.
//
// Agent raportuje ja w heartbeacie, dzieki czemu panel moze pokazac klientowi
// "dostepna nowsza wersja" wraz z komenda, a support widzi, kto na czym siedzi.
// Swiadomie NIE ma tu zadnej automatycznej aktualizacji: to byloby zdalne
// wykonanie kodu na maszynach klientow. Decyzje podejmuje wlasciciel maszyny.
package buildinfo

// Version ustawiane przy budowaniu przez:
//
//	-ldflags "-X github.com/smarthomeentry/agent/internal/buildinfo.Version=1.1.0"
//
// "dev" oznacza budowanie lokalne, poza procesem wydawniczym.
var Version = "dev"
