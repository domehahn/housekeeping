package cli

import "testing"

func TestExecute_VersionReturnsSuccess(t *testing.T) {
	if got := Execute([]string{"version"}); got != ExitSuccess {
		t.Errorf("Execute([version]) = %d, want %d", got, ExitSuccess)
	}
}

func TestExecute_ProviderListReturnsSuccess(t *testing.T) {
	if got := Execute([]string{"provider", "list"}); got != ExitSuccess {
		t.Errorf("Execute([provider list]) = %d, want %d", got, ExitSuccess)
	}
}

func TestExecute_UnknownCommandReturnsGeneralError(t *testing.T) {
	if got := Execute([]string{"this-command-does-not-exist"}); got != ExitGeneralError {
		t.Errorf("Execute([bogus]) = %d, want %d", got, ExitGeneralError)
	}
}

func TestExecute_MissingRequiredArgReturnsGeneralError(t *testing.T) {
	// `execute` requires exactly one plan-file argument.
	if got := Execute([]string{"execute"}); got != ExitGeneralError {
		t.Errorf("Execute([execute]) = %d, want %d", got, ExitGeneralError)
	}
}
