package planning

import (
	"context"
	"fmt"

	"github.com/bengobox/projects-service/internal/ent"
	enttemplate "github.com/bengobox/projects-service/internal/ent/projecttemplate"
	"github.com/bengobox/projects-service/internal/planmodel"
)

// System templates seeded on every start (and by projects-migrate), keyed by system_key: an IT
// rollout, a website build and a consulting engagement. Seeding updates them in place, so a
// change here reaches every tenant on the next rollout.
type systemTemplate struct {
	key, name, description, category string
	def                              planmodel.TemplateDefinition
}

func h(v float64) *float64 { return &v }

func task(ref, title, phase string, offset, days int, est float64, role string, deps ...string) planmodel.TemplateTask {
	return planmodel.TemplateTask{Ref: ref, Title: title, PhaseRef: phase, OffsetDays: offset, DurationDays: days,
		EstimatedHours: h(est), Role: role, DependsOn: deps, Priority: "medium"}
}

const (
	rolePM     = "project_manager"
	roleMember = "member"
)

var systemTemplates = []systemTemplate{
	{
		key: "it_rollout", name: "IT rollout", category: "IT",
		description: "Roll out a system or hardware across sites: assess, plan, pilot, deploy, train and hand over.",
		def: planmodel.TemplateDefinition{
			Phases: []planmodel.TemplatePhase{
				{Ref: "assess", Name: "Assessment signed off", OffsetDays: 9},
				{Ref: "pilot", Name: "Pilot accepted", OffsetDays: 34},
				{Ref: "golive", Name: "Go live", OffsetDays: 55},
				{Ref: "handover", Name: "Handover to support", OffsetDays: 69},
			},
			Tasks: []planmodel.TemplateTask{
				task("t1", "Kick-off and stakeholder map", "assess", 0, 2, 6, rolePM),
				task("t2", "Site and infrastructure survey", "assess", 2, 5, 24, roleMember, "t1"),
				task("t3", "Requirements and success criteria", "assess", 2, 6, 16, rolePM, "t1"),
				task("t4", "Rollout plan, risks and budget", "pilot", 10, 5, 16, rolePM, "t2", "t3"),
				task("t5", "Procure equipment and licences", "pilot", 15, 10, 8, rolePM, "t4"),
				task("t6", "Configure and test in staging", "pilot", 15, 10, 40, roleMember, "t4"),
				task("t7", "Pilot at one site", "pilot", 27, 7, 32, roleMember, "t5", "t6"),
				task("t8", "Deploy to remaining sites", "golive", 35, 15, 80, roleMember, "t7"),
				task("t9", "Train users and administrators", "golive", 42, 10, 24, roleMember, "t7"),
				task("t10", "Hypercare and issue log", "handover", 56, 10, 30, roleMember, "t8"),
				task("t11", "Documentation and support handover", "handover", 63, 6, 12, rolePM, "t10"),
				task("t12", "Close-out report", "handover", 68, 2, 4, rolePM, "t11"),
			},
			Roles: []planmodel.TemplateRole{{Code: rolePM, Label: "Project manager", Count: 1}, {Code: roleMember, Label: "Engineer", Count: 2}},
		},
	},
	{
		key: "website_build", name: "Website build", category: "Digital",
		description: "Design and build a website: discovery, content, design, build, test and launch.",
		def: planmodel.TemplateDefinition{
			Phases: []planmodel.TemplatePhase{
				{Ref: "discovery", Name: "Sitemap approved", OffsetDays: 6},
				{Ref: "design", Name: "Design approved", OffsetDays: 20},
				{Ref: "build", Name: "Site ready for review", OffsetDays: 41},
				{Ref: "launch", Name: "Launch", OffsetDays: 48},
			},
			Tasks: []planmodel.TemplateTask{
				task("t1", "Discovery workshop", "discovery", 0, 2, 6, rolePM),
				task("t2", "Sitemap and user journeys", "discovery", 2, 5, 12, roleMember, "t1"),
				task("t3", "Content plan and copy brief", "design", 7, 5, 10, rolePM, "t2"),
				task("t4", "Wireframes", "design", 7, 6, 20, roleMember, "t2"),
				task("t5", "Visual design and style guide", "design", 13, 8, 32, roleMember, "t4"),
				task("t6", "Build templates and components", "build", 21, 12, 60, roleMember, "t5"),
				task("t7", "Load content and images", "build", 28, 8, 16, roleMember, "t3", "t6"),
				task("t8", "SEO, analytics and forms", "build", 33, 5, 10, roleMember, "t6"),
				task("t9", "Testing on devices and browsers", "launch", 41, 4, 16, roleMember, "t7", "t8"),
				task("t10", "Client review and fixes", "launch", 43, 4, 12, rolePM, "t9"),
				task("t11", "Go live and DNS", "launch", 48, 1, 4, roleMember, "t10"),
			},
			Roles: []planmodel.TemplateRole{{Code: rolePM, Label: "Project manager", Count: 1}, {Code: roleMember, Label: "Designer and developer", Count: 2}},
		},
	},
	{
		key: "consulting_engagement", name: "Consulting engagement", category: "Services",
		description: "Run a consulting assignment: scope, gather evidence, analyse, recommend and close.",
		def: planmodel.TemplateDefinition{
			Phases: []planmodel.TemplatePhase{
				{Ref: "inception", Name: "Inception report", OffsetDays: 9},
				{Ref: "findings", Name: "Draft findings", OffsetDays: 34},
				{Ref: "final", Name: "Final report accepted", OffsetDays: 48},
			},
			Tasks: []planmodel.TemplateTask{
				task("t1", "Engagement letter and kick-off", "inception", 0, 2, 4, rolePM),
				task("t2", "Document review", "inception", 2, 6, 20, roleMember, "t1"),
				task("t3", "Inception report and work plan", "inception", 6, 4, 12, rolePM, "t1"),
				task("t4", "Interviews and site visits", "findings", 10, 10, 40, roleMember, "t3"),
				task("t5", "Data collection and analysis", "findings", 14, 14, 48, roleMember, "t3"),
				task("t6", "Draft findings and recommendations", "findings", 28, 7, 24, rolePM, "t4", "t5"),
				task("t7", "Validation workshop", "final", 36, 2, 8, rolePM, "t6"),
				task("t8", "Final report", "final", 38, 8, 20, rolePM, "t7"),
				task("t9", "Close-out and invoice", "final", 47, 2, 2, rolePM, "t8"),
			},
			Roles: []planmodel.TemplateRole{{Code: rolePM, Label: "Lead consultant", Count: 1}, {Code: roleMember, Label: "Analyst", Count: 2}},
		},
	},
}

// SeedSystemTemplates upserts the system templates by system_key (idempotent; safe on every pod).
func SeedSystemTemplates(ctx context.Context, client *ent.Client) error {
	for _, st := range systemTemplates {
		def := st.def
		if err := ValidateDefinition(&def); err != nil {
			return fmt.Errorf("system template %s: %w", st.key, err)
		}
		existing, err := client.ProjectTemplate.Query().Where(enttemplate.SystemKey(st.key)).Only(ctx)
		switch {
		case ent.IsNotFound(err):
			err = client.ProjectTemplate.Create().SetSystemKey(st.key).SetName(st.name).SetDescription(st.description).
				SetCategory(st.category).SetDefinition(def).SetTaskCount(len(def.Tasks)).Exec(ctx)
			if ent.IsConstraintError(err) {
				err = nil // another pod seeded it a moment ago
			}
		case err == nil:
			err = existing.Update().SetName(st.name).SetDescription(st.description).SetCategory(st.category).
				SetDefinition(def).SetTaskCount(len(def.Tasks)).Exec(ctx)
		}
		if err != nil {
			return fmt.Errorf("seed system template %s: %w", st.key, err)
		}
	}
	return nil
}
