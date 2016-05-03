package main

var (
	emojiMap = map[int]string{
		1: ":one:",
		2: ":two:",
		3: ":three:",
		4: ":four:",
		5: ":five:",
		6: ":six:",
		7: ":seven:",
		8: ":eight:",
		9: ":nine:",
	}

	emojiMapReverse = map[string]int{
		"1\ufe0f\u20e3": 1,
		"2\ufe0f\u20e3": 2,
		"3\ufe0f\u20e3": 3,
		"4\ufe0f\u20e3": 4,
		"5\ufe0f\u20e3": 5,
		"6\ufe0f\u20e3": 6,
		"7\ufe0f\u20e3": 7,
		"8\ufe0f\u20e3": 8,
		"9\ufe0f\u20e3": 9,
	}

	langMapReverse = map[string]string{
		"\U0001f1fa\U0001f1f8": "en",
		"\U0001f1f7\U0001f1fa": "ru",
	}
)
