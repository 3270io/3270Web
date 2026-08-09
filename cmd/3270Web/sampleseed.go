package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// The bundled sample apps, already on the presets page.
//
// Adding one used to be a form: open the dialog, find the app in a list, and
// save a host, a port and a name nobody was offered a choice about. Nothing
// about a sample app preset is a decision — the host, the port and the name
// are fixed, and TLS against a plaintext listener on loopback cannot work —
// so the whole exercise was transcription. An instance with no mainframe to
// reach had to do it eight times before its host list said anything.
//
// So they are seeded instead: every bundled app arrives as a preset the first
// time an administrator opens the presets page, marked not offered. Nobody's
// selection screen changes — a preset that reaches nobody is not on it — and
// what is left for the administrator is the part that is actually a decision:
// which of them to offer, and to whom.
//
// Seeding is recorded per app rather than as a single "done" flag. An app
// removed afterwards stays removed, and an app added by a later release is
// seeded on its own without resurrecting the ones somebody deleted.

const sampleSeedFileName = "sample-presets.json"

type sampleSeedRecord struct {
	// Seeded holds the sample app IDs this instance has already offered to
	// seed. Presence here means "do not add this again", whatever became of
	// the preset afterwards.
	Seeded []string `json:"seeded"`
}

func (r sampleSeedRecord) has(id string) bool {
	for _, seen := range r.Seeded {
		if strings.EqualFold(strings.TrimSpace(seen), id) {
			return true
		}
	}
	return false
}

func readSampleSeedRecord(dir string) (sampleSeedRecord, error) {
	var record sampleSeedRecord
	data, err := os.ReadFile(filepath.Join(dir, sampleSeedFileName))
	if errors.Is(err, os.ErrNotExist) {
		return record, nil
	}
	if err != nil {
		return record, err
	}
	if len(data) == 0 {
		return record, nil
	}
	if err := json.Unmarshal(data, &record); err != nil {
		// A file we cannot read is treated as "nothing seeded yet" would be
		// wrong in exactly the way this record exists to prevent: every
		// deleted sample app would come back. Refusing to seed is the safe
		// direction, and the administrator can still add them by hand.
		return record, fmt.Errorf("read %s: %w", sampleSeedFileName, err)
	}
	return record, nil
}

func writeSampleSeedRecord(dir string, record sampleSeedRecord) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, sampleSeedFileName), data, 0o600)
}

// seedSampleAppPresets adds the bundled sample apps this instance has not
// been offered yet, and returns how many it added.
//
// It runs from the presets page rather than from startup, because that is the
// point at which the store is settled: which store the published set lives in
// depends on whether accounts are separated, and the flat-to-per-user
// migration moves the file. Seeding earlier would mean guessing at both.
func (app *App) seedSampleAppPresets(c *gin.Context) int {
	store, err := app.profileStoreForWrite(c, true)
	if err != nil || store == nil {
		return 0
	}
	dir := filepath.Dir(store.path)

	record, err := readSampleSeedRecord(dir)
	if err != nil {
		log.Printf("sample presets: not seeding: %v", err)
		return 0
	}

	options := sampleAppPresetOptions()
	pending := make([]sampleAppPresetOption, 0, len(options))
	for _, option := range options {
		if !record.has(option.ID) {
			pending = append(pending, option)
		}
	}
	if len(pending) == 0 {
		return 0
	}

	existing, err := store.load()
	if err != nil {
		log.Printf("sample presets: not seeding: could not read the presets: %v", err)
		return 0
	}
	// No prototype games here, but the same reasoning: a preset named
	// "constructor" is a name and not a hit.
	taken := make(map[string]bool, len(existing))
	for _, p := range existing {
		taken[strings.ToLower(strings.TrimSpace(p.Name))] = true
	}

	added := 0
	for _, option := range pending {
		// The store upserts by name, so a sample app whose name collides with
		// a preset somebody wrote would replace it. Skipping is the only safe
		// answer: seeding is something this instance decided to do, and it
		// must not cost anybody a mainframe. It is still recorded as seeded,
		// so the collision is not re-examined on every visit.
		if taken[strings.ToLower(option.Name)] {
			log.Printf("sample presets: not seeding %q — a preset already has that name", option.Name)
			record.Seeded = append(record.Seeded, option.ID)
			continue
		}
		preset := ConnectionProfile{
			Name: option.Name,
			Host: option.Host,
			Port: option.Port,
			// Not offered: it is on the presets page, and on nobody's
			// selection screen until an administrator says so. Seeding a host
			// list onto every account's first screen is not a thing software
			// should do to a running deployment.
			NotOffered: true,
		}
		if err := validateProfile(&preset); err != nil {
			// The bundled apps are ours, so this cannot happen from data —
			// only from a change to one of them. Say so rather than writing a
			// preset that will not connect.
			log.Printf("sample presets: not seeding %q: %v", option.Name, err)
			continue
		}
		if _, err := store.upsert(preset); err != nil {
			log.Printf("sample presets: could not seed %q: %v", option.Name, err)
			continue
		}
		record.Seeded = append(record.Seeded, option.ID)
		added++
	}

	if err := writeSampleSeedRecord(dir, record); err != nil {
		// The presets are written and the record is not, so the next visit
		// would try again — and upsert by name would simply rewrite the same
		// entries, which is harmless. Worth a line either way.
		log.Printf("sample presets: could not record what was seeded: %v", err)
	}
	if added > 0 {
		log.Printf("sample presets: added %d bundled sample app(s), not offered to anybody yet", added)
	}
	return added
}
