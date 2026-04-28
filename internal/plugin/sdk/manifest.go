package sdk

type Manifest struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Description  string   `json:"description,omitempty"`
	Author       string   `json:"author,omitempty"`
	Kind         string   `json:"kind,omitempty"`
	Builtin      bool     `json:"builtin"`
	Runtime      string   `json:"runtime,omitempty"`
	PythonEnv    string   `json:"python_env,omitempty"`
	Entry        string   `json:"entry,omitempty"`
	Args         []string `json:"args,omitempty"`
	Protocol     string   `json:"protocol,omitempty"`
	Source       string   `json:"source,omitempty"`
	ConfigSchema string   `json:"config_schema,omitempty"`
}
