package main

// State is one state the rules prepare a return for, and everything the
// shared files need to know about it.
type State struct {
	// Postal is the state's postal code, in lower case: "vt". It names the
	// rule directory, the oneof fields and the taxprotos package.
	Postal string
	// Name is the state's name: "Vermont". The proto messages are named for
	// it without spaces (VermontInputs, VermontReturn), and so is the rule
	// that prepares the return (prepare-vermont-return).
	Name string
	// Form is the resident return as the state names it: "Form IN-111".
	Form string
	// Field is the state's number in the StateInputs and StateReturn oneofs.
	// It is part of the wire format: never change or reuse one.
	Field int
	// Alias is the name 00_states.tlisp gives the state's taxprotos package.
	Alias string
	// FormDir is the form's directory under us/state/<postal>/v1 in taxprotos.
	FormDir string
	// GoTarget names the form's go_proto_library when FormDir + "_go_proto"
	// would clash with another state's form of the same name.
	GoTarget string
	// HandDispatch is set when the state's dispatch in 00_states.tlisp is
	// written by hand, as Washington's is: its return is due without inputs.
	HandDispatch bool
	// ReturnComment, InputsComment and RustDoc override the comments the
	// generator writes, where the plain form would be wrong or unclear.
	ReturnComment string
	InputsComment string
	RustDoc       string
}

// states is every state with rules of its own, by postal code. The states
// with no income tax share notax/ and need no entry.
var states = []State{
	{Postal: "al", Name: "Alabama", Form: "Form 40", Field: 17, Alias: "al40", FormDir: "form_40", GoTarget: "al_form_40_go_proto"},
	{Postal: "ar", Name: "Arkansas", Form: "Form AR1000F", Field: 18, Alias: "ar1000f", FormDir: "form_ar1000f"},
	{Postal: "az", Name: "Arizona", Form: "Form 140", Field: 16, Alias: "az140", FormDir: "form_140"},
	{Postal: "ca", Name: "California", Form: "Form 540", Field: 10, Alias: "ca540", FormDir: "form_540",
		ReturnComment: "California Form 540 and Schedule CA (540).", InputsComment: "California Form 540 / 540NR inputs."},
	{Postal: "co", Name: "Colorado", Form: "Form DR 0104", Field: 15, Alias: "codr0104", FormDir: "form_dr0104"},
	{Postal: "ct", Name: "Connecticut", Form: "Form CT-1040", Field: 19, Alias: "ct1040", FormDir: "form_ct1040"},
	{Postal: "dc", Name: "District of Columbia", Form: "Form D-40", Field: 21, Alias: "dcd40", FormDir: "form_d40",
		RustDoc: "the District of Columbia's forms."},
	{Postal: "de", Name: "Delaware", Form: "Form PIT-RES", Field: 20, Alias: "depitres", FormDir: "form_pit_res"},
	{Postal: "ga", Name: "Georgia", Form: "Form 500", Field: 22, Alias: "ga500", FormDir: "form_500"},
	{Postal: "hi", Name: "Hawaii", Form: "Form N-11", Field: 23, Alias: "hin11", FormDir: "form_n11"},
	{Postal: "ia", Name: "Iowa", Form: "Form IA 1040", Field: 26, Alias: "ia1040", FormDir: "form_ia1040"},
	{Postal: "id", Name: "Idaho", Form: "Form 40", Field: 24, Alias: "id40", FormDir: "form_40"},
	{Postal: "il", Name: "Illinois", Form: "Form IL-1040", Field: 13, Alias: "il1040", FormDir: "form_il1040"},
	{Postal: "in", Name: "Indiana", Form: "Form IT-40", Field: 25, Alias: "init40", FormDir: "form_it40"},
	{Postal: "ks", Name: "Kansas", Form: "Form K-40", Field: 27, Alias: "ksk40", FormDir: "form_k40"},
	{Postal: "ky", Name: "Kentucky", Form: "Form 740", Field: 28, Alias: "ky740", FormDir: "form_740"},
	{Postal: "la", Name: "Louisiana", Form: "Form IT-540", Field: 29, Alias: "lait540", FormDir: "form_it540"},
	{Postal: "ma", Name: "Massachusetts", Form: "Form 1", Field: 32, Alias: "ma1", FormDir: "form_1", GoTarget: "ma_form_1_go_proto"},
	{Postal: "md", Name: "Maryland", Form: "Form 502", Field: 31, Alias: "md502", FormDir: "form_502"},
	{Postal: "me", Name: "Maine", Form: "Form 1040ME", Field: 30, Alias: "me1040", FormDir: "form_1040me"},
	{Postal: "mi", Name: "Michigan", Form: "Form MI-1040", Field: 33, Alias: "mi1040", FormDir: "form_mi1040"},
	{Postal: "mn", Name: "Minnesota", Form: "Form M1", Field: 34, Alias: "m1", FormDir: "form_m1"},
	{Postal: "mo", Name: "Missouri", Form: "Form MO-1040", Field: 36, Alias: "mo1040", FormDir: "form_mo1040"},
	{Postal: "ms", Name: "Mississippi", Form: "Form 80-105", Field: 35, Alias: "ms80105", FormDir: "form_80105"},
	{Postal: "mt", Name: "Montana", Form: "Form 2", Field: 37, Alias: "mt2", FormDir: "form_2"},
	{Postal: "nc", Name: "North Carolina", Form: "Form D-400", Field: 14, Alias: "ncd400", FormDir: "form_d400"},
	{Postal: "nd", Name: "North Dakota", Form: "Form ND-1", Field: 42, Alias: "nd1", FormDir: "form_nd1"},
	{Postal: "ne", Name: "Nebraska", Form: "Form 1040N", Field: 38, Alias: "ne1040n", FormDir: "form_1040n"},
	{Postal: "nj", Name: "New Jersey", Form: "Form NJ-1040", Field: 39, Alias: "nj1040", FormDir: "form_nj1040"},
	{Postal: "nm", Name: "New Mexico", Form: "Form PIT-1", Field: 40, Alias: "nmpit1", FormDir: "form_pit1"},
	{Postal: "ny", Name: "New York", Form: "Form IT-201", Field: 41, Alias: "nyit201", FormDir: "form_it201"},
	{Postal: "oh", Name: "Ohio", Form: "IT 1040", Field: 43, Alias: "ohit1040", FormDir: "form_it1040"},
	{Postal: "ok", Name: "Oklahoma", Form: "Form 511", Field: 44, Alias: "ok511", FormDir: "form_511"},
	{Postal: "or", Name: "Oregon", Form: "Form OR-40", Field: 11, Alias: "or40", FormDir: "form_or40"},
	{Postal: "pa", Name: "Pennsylvania", Form: "Form PA-40", Field: 45, Alias: "pa40", FormDir: "form_pa40"},
	{Postal: "ri", Name: "Rhode Island", Form: "Form RI-1040", Field: 46, Alias: "ri1040", FormDir: "form_ri1040"},
	{Postal: "sc", Name: "South Carolina", Form: "SC1040", Field: 47, Alias: "sc1040", FormDir: "form_sc1040"},
	{Postal: "ut", Name: "Utah", Form: "Form TC-40", Field: 48, Alias: "uttc40", FormDir: "form_tc40"},
	{Postal: "va", Name: "Virginia", Form: "Form 760", Field: 50, Alias: "va760", FormDir: "form_760"},
	{Postal: "vt", Name: "Vermont", Form: "Form IN-111", Field: 49, Alias: "vtin111", FormDir: "form_in111"},
	{Postal: "wa", Name: "Washington", Form: "capital gains excise tax return", Field: 12, Alias: "wacg", FormDir: "capital_gains",
		HandDispatch:  true,
		ReturnComment: "Washington long-term capital gains excise tax return.",
		InputsComment: "Washington excise tax on long-term capital gains inputs.",
		RustDoc:       "Washington's capital gains excise\n/// tax return."},
	{Postal: "wi", Name: "Wisconsin", Form: "Form 1", Field: 52, Alias: "wi1", FormDir: "form_1", GoTarget: "form_1_wi_go_proto"},
	{Postal: "wv", Name: "West Virginia", Form: "Form IT-140", Field: 51, Alias: "wvit140", FormDir: "form_it140"},
}
