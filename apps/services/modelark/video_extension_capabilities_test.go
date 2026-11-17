package modelark

import "testing"

func TestVideoExtensionCapabilityIsProviderSpecific(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"dreamina-seedance-2-0-fast-260128", "dreamina-seedance-2-0-260128", "dreamina-seedance-2-5-260628"} {
		m, ok := ProviderModel(id)
		if !ok || !m.SupportsVideoExtension {
			t.Fatalf("documented extension provider missing capability %s", id)
		}
	}
	m, ok := ProviderModel("seedance-1-0-pro-250528")
	if !ok || m.SupportsVideoExtension {
		t.Fatal("legacy image-video capability must not imply video extension")
	}
}
