package daemon

import (
	"math/rand/v2"
	"strconv"
)

// tabNames are what a tab is called when the user doesn't name it: words
// ending in "-ite", like osmite itself. Real words, a few minerals, and some
// made-up ones; anything that sounds right with -ite.
var tabNames = []string{
	// Everyday words.
	"finite", "infinite", "polite", "satellite", "appetite", "dynamite",
	"termite", "campsite", "favorite", "opposite", "exquisite", "ignite",
	"unite", "invite", "recite", "excite", "erudite", "sprite", "kite",
	"elite", "suite", "rewrite", "overwrite", "website",
	"meteorite", "kryptonite", "graphite", "granite", "quartzite",
	"marmite", "vegemite", "socialite", "parasite", "midnite",
	// Made up, for a terminal full of agents and code.
	"claudite", "codexite", "promptite", "tokenite", "pixelite", "kernelite",
	"socketite", "bufferite", "cachite", "commitite", "branchite", "mergite",
	"compilite", "debuggite", "lambdite", "vectorite", "nebulite", "cometite",
	"lunite", "solarite", "starlite", "orbitite", "quantite", "fluxite",
	"zenite", "manite", "neonite", "echoite", "glitchite", "ozonite",
}

// randomTabName picks a name not already used in ws, adding a number only if
// every word is taken.
func randomTabName(ws *Workspace) string {
	used := map[string]bool{}
	for _, t := range ws.tabs {
		used[t.name] = true
	}
	start := rand.IntN(len(tabNames))
	for i := range tabNames {
		if n := tabNames[(start+i)%len(tabNames)]; !used[n] {
			return n
		}
	}
	for i := 2; ; i++ {
		n := tabNames[start] + "-" + strconv.Itoa(i)
		if !used[n] {
			return n
		}
	}
}
