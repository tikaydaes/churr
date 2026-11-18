package dice

import "github.com/tikaydaes/churr/internal/model"

// What /help says of /roll and /dice. {cmd} is the command's name here and
// {cmd:roll} the name /roll has here; the server writes them in.

var rollHelp = model.CommandHelp{
	Description: "Rolls dice, rolled by the server, so everyone sees the same result and nobody could have made it up or edited it later. The result shows each die, any dropped ones greyed and struck through, and the total; it spins once for everyone who sees it arrive. Separate rolls with commas to roll several rows at once, each on its own line with its own total. You can also roll inside a sentence with [[2d6+3]] (one row only).",
	Args:        []model.CommandHelpArg{{Name: "dice", About: "Nothing rolls one twenty-sided die (d20). NdS rolls N dice of S sides: N may be left out, d% is d100, and a die has 2 to 1,000 sides (a d2 is drawn as a coin). Add kh, kl, dh or dl and a count to keep or drop the highest or lowest, as in 4d6kh3 (k alone keeps the highest, so 3d10k2 drops the lowest). Join terms with + and - and add plain numbers, as in 2d6+3 or d20+1d4-1. adv and dis roll two d20s and keep the higher or the lower. Commas separate rows, as in 4d4+d6, 3d10k2. At most 8 rows, 10 terms a row, 100 dice in all, numbers up to 10,000 and 100 characters."}},
	Examples: []model.CommandExample{
		{Type: "/{cmd}", Then: "Rolls one d20."},
		{Type: "/{cmd} 2d6+3", Then: "Rolls two six-sided dice and adds 3."},
		{Type: "/{cmd} 4d6kh3", Then: "Rolls four six-sided dice and keeps the highest three; the lowest is shown struck through."},
		{Type: "/{cmd} adv", Then: "Rolls with advantage: two d20s, keeping the higher."},
		{Type: "/{cmd} 4d4+d6, 3d10k2", Then: "Rolls two rows: four four-sided dice and a six-sided one added up, then three ten-sided dice keeping the highest two."},
	},
	Where: "Channels and chats. Typed in a thread, the roll goes in the thread. Not in sealed chats.",
	Who:   "Anyone who can post. Not bots.",
}

var diceHelp = model.CommandHelp{
	Description: "Rolls six-sided dice, rolled by the server, the same as /{cmd:roll} with a number of d6: /{cmd} 4 is /{cmd:roll} 4d6. Everything /{cmd:roll} takes works here too, rows separated by commas included.",
	Args:        []model.CommandHelpArg{{Name: "dice", About: "Nothing rolls one d6. A bare number at the start of a row is that many d6; a number after it is added (4+3 is 4d6+3). Anything written with a d is as /{cmd:roll} reads it (2d8 is 2d8)."}},
	Examples: []model.CommandExample{
		{Type: "/{cmd}", Then: "Rolls one six-sided die."},
		{Type: "/{cmd} 4", Then: "Rolls four six-sided dice, the same as /{cmd:roll} 4d6."},
		{Type: "/{cmd} 4+3", Then: "Rolls four six-sided dice and adds 3."},
		{Type: "/{cmd} 4, 2+1", Then: "Rolls two rows: four d6, then two d6 plus 1."},
	},
	Where: "Channels and chats. Typed in a thread, the roll goes in the thread. Not in sealed chats.",
	Who:   "Anyone who can post. Not bots.",
}

var flipHelp = model.CommandHelp{
	Description: "Flips a coin, flipped by the server: heads or tails, the same for everyone and impossible to edit. The server proves the flip, and the shield beside it says whether this app checked the proof. Pressing the result spins it again, but only for show: the outcome does not change.",
	Examples: []model.CommandExample{
		{Type: "/{cmd}", Then: "Posts heads or tails."},
		{Type: "/{cmd} (in a thread)", Then: "Flips in the thread, so the answer stays with the question."},
	},
	Where: "Channels and chats. Not in sealed chats.",
	Who:   "Anyone who can post. Not bots.",
}
