package model

// FileCall holds parsed data for a file provider step. Path is the file to
// inspect (resolved against scenario.workdir, or under project.dir when given
// as ${project.dir}/...). Expect holds the optional assertions.
type FileCall struct {
	Path   Expression
	Expect *FileExpect
}

// FileExpect holds the file provider assertions. Each field is optional; only
// the set ones run. Hashes maps an algorithm name (sha1, sha224, sha256,
// sha384, sha512, sha512_224, sha512_256) to its expected digest expression.
type FileExpect struct {
	Exists    Expression
	SizeBytes Expression
	Text      Expression
	JSON      Expression
	Hashes    map[string]Expression
}
