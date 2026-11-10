package allisall

import "maps"

// ToImportMapping returns the mapping an import ran with, read from
// [ImportsService.Get], as the Mapping of the next [ImportUpload].
//
// It copies the four members and refuses nothing. A delimiter this SDK
// declares no constant for is sent as it is, and the API answers it as it
// answers any mapping. A nil Columns or Constants is sent as an empty
// object, never as null.
func (m AppliedImportMapping) ToImportMapping() ImportMapping {
	columns := cloneOrEmpty(m.Columns)
	constants := cloneOrEmpty(m.Constants)
	timezone := m.Timezone
	delimiter := m.Delimiter
	return ImportMapping{
		Columns:   &columns,
		Constants: &constants,
		Timezone:  &timezone,
		Delimiter: &delimiter,
	}
}

// cloneOrEmpty copies m, giving an empty map for a nil one, which
// encoding/json writes as {} rather than null.
func cloneOrEmpty(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return maps.Clone(m)
}
