/*  Copyright (c) 2013, Brian Hummer (brian@boggo.net)
All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:
    * Redistributions of source code must retain the above copyright
      notice, this list of conditions and the following disclaimer.
    * Redistributions in binary form must reproduce the above copyright
      notice, this list of conditions and the following disclaimer in the
      documentation and/or other materials provided with the distribution.
    * Neither the name of the boggo.net nor the
      names of its contributors may be used to endorse or promote products
      derived from this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL BRIAN HUMMER BE LIABLE FOR ANY
DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
(INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES;
LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND
ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
*/

package neat

import (
	"fmt"
	"sort"
)

type Population struct {
	Generation int          // Current generation
	Species    SpeciesSlice // The species which make up the population
}

func (pop Population) String() string {
	return fmt.Sprintf("Population: Generation is %d with %d Species", pop.Generation, len(pop.Species))
}

// Creates the initial population from the settings by cloning the initial genome
func initialPopulation(settings *Settings, inno *innovation) (pop *Population, err error) {

	// The initial population has only one species
	pop = &Population{Generation: 1, Species: make([]*Species, 1, 10)}
	pop.Species[0] = &Species{ID: inno.nextID()}
	pop.Species[0].Orgs = make([]*Organism, settings.PopulationSize)

	// Fill the species with copies of the initial genome
	ig, e2 := initialGenome(settings, inno)
	if e2 != nil {
		err = e2
		return
	}
	for i := 0; i < settings.PopulationSize; i++ {
		g := cloneGenome(ig, inno.nextID())
		for _, cg := range g.Conns {
			cg.Weight = random.Gaussian()
		}
		pop.Species[0].Orgs[i] = &Organism{Genome: g}
	}

	return
}

// Rolls a population to the next generation
func rollPop(settings *Settings, inno *innovation, population *Population) (nextPop *Population, err error) {

	// Construct the next population
	currPop := population
	nextPop = &Population{Generation: currPop.Generation + 1,
		Species: make([]*Species, 0, len(currPop.Species))}

	// Update the species fitness in the current population
	var bestSpecies *Species
	//var bestOrg *Organism
	var bestFit float64
	for _, s := range currPop.Species {
		s.calcFitness()
		for _, o := range s.Orgs {
			if o.Fitness[0] > bestFit {
				//bestOrg = o
				bestFit = o.Fitness[0]
				bestSpecies = s
			}
		}
	}

	// Allow viable species to continue to live but cull their numbers
	adjFit := float64(0)
	popFit := float64(0)
	var living SpeciesSlice
	living = make([]*Species, 0, len(currPop.Species))
	for _, s := range currPop.Species {
		if s.ID == bestSpecies.ID || s.Age-s.BestFitAge < settings.AgeToStagnation {
			living = append(living, s)
			adjFit += s.currFitness
			sort.Sort(sort.Reverse(s.Orgs))
			keep := int(settings.SurvivalPercent * float64(len(s.Orgs)))
			if keep < settings.EliteCount {
				keep = settings.EliteCount
			}
			if keep > len(s.Orgs) {
				keep = len(s.Orgs)
			}
			s.Orgs = s.Orgs[:keep]
			popFit += s.Orgs.TotalFitness()
			s.Example = s.Orgs[random.Int(keep)]
		}
	}
	//sort.Sort(sort.Reverse(living)) // Reverse sort by best fitness
	popOrgs := living.Organisms(settings)

	// Create the next generation
	inno.reset()
	children := make([]*Organism, 0, settings.PopulationSize) // TODO: Make this a channel for concurrency support
	for _, currS := range living {

		// Copy the species to the next generation
		cnt := int(currS.currFitness / adjFit * float64(settings.PopulationSize))
		nextS := &Species{ID: currS.ID, Orgs: make([]*Organism, 0, cnt), Age: currS.Age + 1,
			BestFitness: currS.BestFitness, BestFitAge: currS.BestFitAge, Example: currS.Example}
		nextPop.Species = append(nextPop.Species, nextS)

		// Add the elite
		for i := 0; i < settings.EliteCount && i < len(currS.Orgs); i++ {
			children = append(children, currS.Orgs[i])
			cnt -= 1
		}

		// Create the offspring
		orgFit := currS.Orgs.TotalFitness()
		for i := 0; i < cnt; i++ {

			// Allow for innerspecies mating. This is done simply by skipping
			// over this request for an offspring and letting the section
			// below, "Ensure we have the right number of children", create
			// the (potentionally) interspecies child
			if random.Float64() < settings.InterspeciesMating {
				continue
			}

			// Select parent 1
			p1 := tournament(currS.Orgs, orgFit)

			// Mutate only
			if len(currS.Orgs) == 1 || random.Next() > settings.Crossover {
				child := cloneOrg(p1, inno.nextID())
				mutate(settings, inno, child)
				children = append(children, child)
			} else {

				// Pick a mate
				var p2 *Organism
				if random.Next() < settings.InterspeciesMating {
					p2 = tournament(popOrgs, popFit)
				} else {
					p2 = tournament(currS.Orgs, orgFit)
				}

				// Crossover and mutate
				child := crossover(inno, p1, p2)
				mutate(settings, inno, child)
				children = append(children, child)
			}
		}

		// Ensure we have the right number of children
		if len(children) > settings.PopulationSize {
			children = children[:settings.PopulationSize]
		} else {
			cnt = settings.PopulationSize - len(children)
			for c := 0; c < cnt; c++ {
				p1 := tournament(popOrgs, popFit)
				p2 := tournament(popOrgs, popFit)
				child := crossover(inno, p1, p2)
				mutate(settings, inno, child)
				children = append(children, child)
			}
		}

	}

	// Speciate the children
	speciate(settings, inno, nextPop, children)

	// Prune off species which are empty
	living = make([]*Species, 0, len(living))
	for _, s := range nextPop.Species {
		if len(s.Orgs) > 0 {
			living = append(living, s)
		}
	}
	nextPop.Species = living

	// Replace the current population with the next one
	return

}

func tournament(orgs []*Organism, totFit float64) (champ *Organism) {
	tgt := random.Next() * totFit
	sum := float64(0)
	for _, o := range orgs {
		sum += o.Fitness[0]
		if sum >= tgt {
			champ = o
			return
		}
	}
	return // Should be an error to get here
}

func speciate(settings *Settings, inno *innovation, pop *Population, children OrganismSlice) {

	// Iterate the children
	for _, child := range children {

		// Iterate the species
		found := false
		for _, s := range pop.Species {
			d := distance(settings, child, s.Example)
			if d < settings.CompatThreshold {
				s.Orgs = append(s.Orgs, child)
				found = true
				break
			}
		}

		// No species found, add a new one
		if !found {
			newS := &Species{ID: inno.nextID(), Orgs: make([]*Organism, 0, 10)}
			pop.Species = append(pop.Species, newS)

			newS.Orgs = append(newS.Orgs, child)
			newS.Example = child
		}
	}
}

func (pop *Population) Organisms() OrganismSlice {
	n := 0
	for _, s := range pop.Species {
		n += len(s.Orgs)
	}

	orgs := make([]*Organism, 0, n)
	for _, s := range pop.Species {
		for _, o := range s.Orgs {
			orgs = append(orgs, o)
		}
	}

	return orgs
}

// Returns the mean population complextiy
// Defined at http://sharpneat.sourceforge.net/phasedsearch.html
func (pop *Population) MPC() float64 {

	tot := 0
	cnt := 0
	for _, s := range pop.Species {
		for _, o := range s.Orgs {
			tot += len(o.Nodes) + len(o.Conns)
		}
		cnt += 1
	}

	return float64(tot) / float64(cnt)
}
