# Trailer-strip and Cursor-identity git config writes shared by the commit hooks.
# The six git config writes do not rewrite the commit running the hook; they
# persist for later commits, including after commit-msg rejects.

TARGET_NAME="regutierrez"
TARGET_EMAIL="rpegutierrez@gmail.com"

is_cursor() {
	printf '%s' "$1" | grep -qiE 'cursor[[:space:]]+agent|cursoragent@cursor\.com'
}

cfg() {
	git config --get "$1" || true
}

rewrite_cursor_identity() {
	an="${GIT_AUTHOR_NAME:-$(cfg author.name)}"
	[ -n "$an" ] || an="$(cfg user.name)"
	ae="${GIT_AUTHOR_EMAIL:-$(cfg author.email)}"
	[ -n "$ae" ] || ae="$(cfg user.email)"
	cn="${GIT_COMMITTER_NAME:-$(cfg committer.name)}"
	[ -n "$cn" ] || cn="$(cfg user.name)"
	ce="${GIT_COMMITTER_EMAIL:-$(cfg committer.email)}"
	[ -n "$ce" ] || ce="$(cfg user.email)"

	if is_cursor "$an" || is_cursor "$ae" || is_cursor "$cn" || is_cursor "$ce"; then
		git config user.name "$TARGET_NAME"
		git config user.email "$TARGET_EMAIL"
		git config author.name "$TARGET_NAME"
		git config author.email "$TARGET_EMAIL"
		git config committer.name "$TARGET_NAME"
		git config committer.email "$TARGET_EMAIL"
	fi
}

strip_cursor_trailers() {
	tmp=$(mktemp)
	trap 'rm -f "$tmp"' EXIT

	if ! grep -v -E \
		-e '^[[:space:]]*Co-authored-by:.*[Cc]ursor' \
		-e '^[[:space:]]*Made-with:[[:space:]]*Cursor([[:space:]]|$)' \
		-e '^[[:space:]]*Made with Cursor([[:space:]]|$)' \
		-e 'cursoragent@cursor\.com' \
		"$1" > "$tmp"
	then
		# grep -v exits 1 when every line matched (strip-to-empty).
		:
	fi
	cat "$tmp" > "$1"
}
