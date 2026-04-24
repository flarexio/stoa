package icd

import "sort"

// Dictionary is the allowed ICD-10 code set the agent may draw from.
// Keeping it an interface lets callers swap in smaller or larger code sets
// without touching domain rules.
type Dictionary interface {
	Lookup(code string) (description string, ok bool)
	Codes() []string
}

// InMemoryDictionary is a stdlib-only Dictionary backed by a Go map. It ships
// with icd as the default in-process implementation, the way io.Discard ships
// with io -- external storage (Postgres, Redis, file-based) belongs in a
// sibling subpackage, not here.
type InMemoryDictionary map[string]string

func (d InMemoryDictionary) Lookup(code string) (string, bool) {
	desc, ok := d[code]
	return desc, ok
}

func (d InMemoryDictionary) Codes() []string {
	codes := make([]string, 0, len(d))
	for c := range d {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	return codes
}

// DefaultDictionary returns a small set of common outpatient ICD-10 codes.
// It is deliberately narrow so the feature loop can be exercised without
// shipping a full terminology file.
func DefaultDictionary() InMemoryDictionary {
	return InMemoryDictionary{
		"I10":     "Essential (primary) hypertension",
		"I25.10":  "Atherosclerotic heart disease of native coronary artery without angina pectoris",
		"I48.91":  "Unspecified atrial fibrillation",
		"I50.9":   "Heart failure, unspecified",
		"E11.9":   "Type 2 diabetes mellitus without complications",
		"E11.65":  "Type 2 diabetes mellitus with hyperglycemia",
		"E78.5":   "Hyperlipidemia, unspecified",
		"E66.9":   "Obesity, unspecified",
		"E03.9":   "Hypothyroidism, unspecified",
		"J45.909": "Unspecified asthma, uncomplicated",
		"J44.9":   "Chronic obstructive pulmonary disease, unspecified",
		"J18.9":   "Pneumonia, unspecified organism",
		"J06.9":   "Acute upper respiratory infection, unspecified",
		"N39.0":   "Urinary tract infection, site not specified",
		"N18.3":   "Chronic kidney disease, stage 3",
		"K21.9":   "Gastro-esophageal reflux disease without esophagitis",
		"K59.00":  "Constipation, unspecified",
		"K29.70":  "Gastritis, unspecified, without bleeding",
		"M54.5":   "Low back pain",
		"M25.50":  "Pain in unspecified joint",
		"M79.3":   "Panniculitis, unspecified",
		"G43.909": "Migraine, unspecified, not intractable, without status migrainosus",
		"G47.00":  "Insomnia, unspecified",
		"F41.9":   "Anxiety disorder, unspecified",
		"F32.9":   "Major depressive disorder, single episode, unspecified",
		"F17.210": "Nicotine dependence, cigarettes, uncomplicated",
		"R51":     "Headache",
		"R07.9":   "Chest pain, unspecified",
		"R10.9":   "Unspecified abdominal pain",
		"R05":     "Cough",
	}
}
