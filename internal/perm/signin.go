package perm

// More things an admin does to another account (docs/AUTH.md sections 9.4
// and 10.5). CheckOnAccount answers them like the others: admins act on
// members, guests and bots, only the owner acts on an admin, and nobody
// does them to the owner.
const (
	SignOutAccount AccountAction = "sign_out_account"
	ResetTotp      AccountAction = "reset_totp"
)
