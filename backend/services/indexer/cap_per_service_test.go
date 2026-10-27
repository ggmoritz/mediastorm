package indexer

import (
	"testing"

	"novastream/models"
)

// Hybrid ranking puts the preferred service first; the cap must still leave
// the other service's results in the list.
func TestCapScoredResultsKeepsEachServiceInHybrid(t *testing.T) {
	var results []models.ScoredNZBResult
	for range 4 {
		results = append(results, models.ScoredNZBResult{NZBResult: models.NZBResult{ServiceType: models.ServiceTypeDebrid}})
	}
	for range 3 {
		results = append(results, models.ScoredNZBResult{NZBResult: models.NZBResult{ServiceType: models.ServiceTypeUsenet}})
	}

	capped := capScoredResults(results, 2)
	counts := map[models.ContentServiceType]int{}
	for _, r := range capped {
		counts[r.ServiceType]++
	}
	if counts[models.ServiceTypeDebrid] != 2 || counts[models.ServiceTypeUsenet] != 2 {
		t.Fatalf("expected 2 results per service, got %v", counts)
	}
	if capped[0].ServiceType != models.ServiceTypeDebrid || capped[2].ServiceType != models.ServiceTypeUsenet {
		t.Fatalf("expected ranking order preserved, got %v", capped)
	}

	single := capScoredResults(results[:4], 2)
	if len(single) != 2 {
		t.Fatalf("expected single-service cap of 2, got %d", len(single))
	}
}
