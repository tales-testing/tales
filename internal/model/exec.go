package model

// ExecCall holds parsed data for an exec provider step. Command is required;
// the rest are optional. Tales calls the executable directly with Args — it
// never interprets a shell, so command = "bash" args = ["script.sh"] runs
// bash, but command = "bash -c ..." is treated as a literal program name.
type ExecCall struct {
	Command Expression
	Args    Expression
	Env     Expression
	Stdin   Expression
	Timeout Expression
	Sandbox *ExecSandbox
	Expect  *ExecExpect
}

// ExecSandbox holds the optional sandbox block. In V1 mode "process" is a soft
// sandbox (working directory, environment, timeout, output capture) and is NOT
// a security boundary; mode "docker" is reserved but not implemented.
type ExecSandbox struct {
	Mode    Expression
	Workdir Expression
	Env     Expression
	Network Expression
}

// ExecExpect holds the exec provider assertions. exit_code matches the process
// exit status; stdout / stderr match the captured streams (matchers welcome);
// stdout_json matches the parsed stdout JSON object.
type ExecExpect struct {
	ExitCode   Expression
	Stdout     Expression
	Stderr     Expression
	StdoutJSON Expression
}
