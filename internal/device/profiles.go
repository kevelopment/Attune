package device

import "strings"

// DeviceProfile describes conservative transfer defaults for a device family.
// It is separate from the transport used to connect to the device: a profile
// must not imply that a transport (USB storage or MTP) is available.
type DeviceProfile struct {
	ID                        string
	Name                      string
	BookFolders               []string
	TransferFormats           []string
	AllowEPUBToMOBI           bool
	RequiresExplicitSelection bool
}

// DeviceDescriptor contains the small set of identifying strings exposed by
// mounted-volume and device transports.
type DeviceDescriptor struct {
	Label   string
	Name    string
	Vendor  string
	Product string
}

type profileDefinition struct {
	profile DeviceProfile
	tokens  []string
}

// The ordered format lists are transfer preferences, not certification or
// proof that every model/firmware can open every listed format. They favor
// common, unprotected sideload formats and intentionally omit DRM/KFX. Folder
// lists are candidate destinations only; callers should confirm a destination
// when it is not already known for that device. Per-device overrides take
// precedence over these defaults.
var profileDefinitions = []profileDefinition{
	{
		profile: DeviceProfile{
			ID: "kindle", Name: "Kindle", BookFolders: []string{"documents", "Documents"},
			TransferFormats: []string{"azw3", "mobi", "prc"}, AllowEPUBToMOBI: true,
		},
		tokens: []string{"kindle", "amazon"},
	},
	{
		profile: DeviceProfile{
			ID: "kobo", Name: "Kobo", BookFolders: []string{"Books", "book", "Documents"},
			TransferFormats: []string{"epub", "pdf", "cbz"},
		},
		tokens: []string{"kobo"},
	},
	{
		profile: DeviceProfile{
			ID: "pocketbook", Name: "PocketBook", BookFolders: []string{"Books", "Documents"},
			TransferFormats: []string{"epub", "fb2", "pdf", "txt", "mobi", "cbz", "cbr"},
		},
		tokens: []string{"pocketbook", "pocket book"},
	},
	{
		profile: DeviceProfile{
			ID: "tolino", Name: "Tolino", BookFolders: []string{"Books", "Documents"},
			TransferFormats: []string{"epub", "pdf"},
		},
		tokens: []string{"tolino"},
	},
	{
		profile: DeviceProfile{
			ID: "nook", Name: "NOOK", BookFolders: []string{"Books", "Documents"},
			TransferFormats: []string{"epub", "pdf"},
		},
		tokens: []string{"nook"},
	},
	{
		profile: DeviceProfile{
			ID: "boox-android", Name: "BOOX / Android", BookFolders: []string{"Books", "Documents", "Download"},
			TransferFormats: []string{"epub", "pdf", "txt"},
		},
		tokens: []string{"boox", "onyx"},
	},
	{
		profile: DeviceProfile{
			ID: "supernote", Name: "Supernote", BookFolders: []string{"Books", "Documents"},
			TransferFormats: []string{"epub", "pdf", "txt"},
		},
		tokens: []string{"supernote"},
	},
}

// ResolveProfile returns the first matching compiled profile or a generic
// storage profile. Generic devices require an explicit profile/destination
// choice before writing; their supported formats are deliberately unspecified.
func ResolveProfile(descriptor DeviceDescriptor) DeviceProfile {
	text := strings.ToLower(strings.Join([]string{
		descriptor.Label, descriptor.Name, descriptor.Vendor, descriptor.Product,
	}, " "))
	for _, definition := range profileDefinitions {
		for _, token := range definition.tokens {
			if strings.Contains(text, token) {
				return cloneProfile(definition.profile)
			}
		}
	}
	return GenericProfile()
}

// GenericProfile returns a safe fallback for unrecognized mounted storage.
func GenericProfile() DeviceProfile {
	return DeviceProfile{
		ID: "generic", Name: "Generic storage",
		BookFolders:               []string{"Books", "Documents"},
		RequiresExplicitSelection: true,
	}
}

func cloneProfile(profile DeviceProfile) DeviceProfile {
	profile.BookFolders = append([]string(nil), profile.BookFolders...)
	profile.TransferFormats = append([]string(nil), profile.TransferFormats...)
	return profile
}
