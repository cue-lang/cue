#Config: {
	rules?: [...#Rule]
}

#Rule: {
	effect!:           "allow" | "deny"
	cueModule?:        string
	cueModuleVersion?: string
	goModule?:         string
	goModuleVersion?:  string
	description?:      string
}
