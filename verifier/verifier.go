package verifier

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"strings"
	"time"

	gh "github.com/google/go-github/v83/github"

	"github.com/shugen002/RepoCaptchaBot/models"
	"github.com/shugen002/RepoCaptchaBot/utils"
)

type Question struct {
	Generator string
	Type      string
	Question  string
	Data      map[string]interface{}
}

func (q Question) Prompt() string {
	if strings.TrimSpace(q.Question) != "" {
		return q.Question
	}
	if q.Data == nil {
		return ""
	}
	prompt, ok := q.Data["prompt"]
	if !ok {
		return ""
	}
	text, ok := prompt.(string)
	if !ok {
		return ""
	}
	return text
}

var ErrUnknownQuestionType = errors.New("unknown question type")

type Verifier struct {
	gh  *gh.Client
	db  *models.Store
	rnd *rand.Rand
}

func NewVerifier(gh *gh.Client, store *models.Store) *Verifier {
	return &Verifier{
		gh:  gh,
		db:  store,
		rnd: rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), uint64(time.Now().UnixNano())+1)),
	}
}

type questionGenerator interface {
	name() string
	types() []string
	generate(qType string) (Question, bool, error)
	verify(question *Question, answer string) bool
}

type basicGenerator struct {
	nameStr    string
	typesList  []string
	generateFn func(qType string) (Question, bool, error)
	verifyFn   func(question *Question, answer string) bool
}

func (g *basicGenerator) name() string {
	return g.nameStr
}

func (g *basicGenerator) types() []string {
	return g.typesList
}

func (g *basicGenerator) generate(qType string) (Question, bool, error) {
	return g.generateFn(qType)
}

func (g *basicGenerator) verify(question *Question, answer string) bool {
	if g.verifyFn == nil {
		return false
	}
	return g.verifyFn(question, answer)
}

func (v *Verifier) questionGenerators(ctx context.Context, cfg models.ChatConfig, i18n *utils.I18n) []questionGenerator {
	defaultVerify := func(question *Question, answer string) bool {
		return verifyByExpectedAnswer(question, answer)
	}
	return []questionGenerator{
		v.commitInfoGenerator(cfg, i18n),
		v.repoInfoGenerator(cfg, i18n),
		v.singleTypeGenerator(ctx, cfg, i18n, "latest_release", v.questionLatestRelease, defaultVerify),
		v.singleTypeGenerator(ctx, cfg, i18n, "file_line", v.questionFileLine, defaultVerify),
	}
}

func (v *Verifier) commitInfoGenerator(cfg models.ChatConfig, i18n *utils.I18n) questionGenerator {
	repo := strings.TrimSpace(cfg.Repo)
	return &commitInfoGenerator{gh: v.gh, repo: repo, i18n: i18n}
}

func (v *Verifier) repoInfoGenerator(cfg models.ChatConfig, i18n *utils.I18n) questionGenerator {
	repo := strings.TrimSpace(cfg.Repo)
	return &repoInfoGenerator{gh: v.gh, repo: repo, i18n: i18n}
}

func (v *Verifier) singleTypeGenerator(ctx context.Context, cfg models.ChatConfig, i18n *utils.I18n, qType string, genFn func(context.Context, models.ChatConfig, *utils.I18n) (Question, bool, error), verifyFn func(*Question, string) bool) questionGenerator {
	qType = strings.ToLower(strings.TrimSpace(qType))
	return &basicGenerator{
		nameStr:   qType,
		typesList: []string{qType},
		generateFn: func(t string) (Question, bool, error) {
			t = strings.ToLower(strings.TrimSpace(t))
			if t != qType {
				return Question{}, false, ErrUnknownQuestionType
			}
			return genFn(ctx, cfg, i18n)
		},
		verifyFn: verifyFn,
	}
}

func (v *Verifier) GenerateQuestion(ctx context.Context, cfg models.ChatConfig, i18n *utils.I18n) (Question, error) {
	if strings.TrimSpace(cfg.Repo) == "" {
		return Question{}, errors.New("repo 未配置")
	}
	generators := v.questionGenerators(ctx, cfg, i18n)

	available := make([]Question, 0, len(generators))
	for _, gen := range generators {
		for _, qType := range gen.types() {
			q, ok, err := gen.generate(qType)
			if err != nil {
				continue
			}
			if ok {
				v.finalizeQuestion(&q, gen, qType)
				available = append(available, q)
			}
		}
	}

	if len(available) == 0 {
		return Question{}, errors.New("无法生成题目")
	}

	idx := v.rnd.IntN(len(available))
	return available[idx], nil
}

func (v *Verifier) GenerateQuestionByType(ctx context.Context, cfg models.ChatConfig, i18n *utils.I18n, qType string) (Question, bool, error) {
	qType = strings.TrimSpace(strings.ToLower(qType))
	if qType == "" {
		return Question{}, false, errors.New("type 不能为空")
	}
	for _, gen := range v.questionGenerators(ctx, cfg, i18n) {
		if !containsType(gen.types(), qType) {
			continue
		}
		q, ok, err := gen.generate(qType)
		if err != nil {
			return Question{}, false, err
		}
		if ok {
			v.finalizeQuestion(&q, gen, qType)
		}
		return q, ok, nil
	}
	return Question{}, false, ErrUnknownQuestionType
}

func (v *Verifier) SupportedQuestionTypes() []string {
	gens := v.questionGenerators(context.Background(), models.ChatConfig{}, nil)
	types := make([]string, 0, len(gens))
	for _, gen := range gens {
		types = append(types, gen.types()...)
	}
	return types
}

func (v *Verifier) AvailableQuestionTypes(ctx context.Context, cfg models.ChatConfig, i18n *utils.I18n) ([]string, error) {
	generators := v.questionGenerators(ctx, cfg, i18n)
	available := make([]string, 0, len(generators))
	for _, gen := range generators {
		for _, qType := range gen.types() {
			_, ok, err := gen.generate(qType)
			if err != nil {
				continue
			}
			if ok {
				available = append(available, qType)
			}
		}
	}
	return available, nil
}

func (v *Verifier) VerifyQuestion(question Question, answer string) bool {
	generators := v.questionGenerators(context.Background(), models.ChatConfig{}, nil)
	gen := findGenerator(generators, question.Generator, question.Type)
	if gen == nil {
		return false
	}
	return gen.verify(&question, answer)
}

func (v *Verifier) FromStoredQuestion(q models.StoredQuestion) Question {
	return v.storedQuestionToQuestion(q)
}

func EncodeQuestionData(q Question) (string, error) {
	data := make(map[string]interface{}, len(q.Data)+2)
	for key, value := range q.Data {
		data[key] = value
	}
	if q.Question != "" {
		if _, ok := data["prompt"]; !ok {
			data["prompt"] = q.Question
		}
	}
	if q.Generator != "" {
		if _, ok := data["generator"]; !ok {
			data["generator"] = q.Generator
		}
	}
	if q.Type != "" {
		if _, ok := data["type"]; !ok {
			data["type"] = q.Type
		}
	}
	if len(data) == 0 {
		return "{}", nil
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func ExtractAnswer(q Question) string {
	if q.Data == nil {
		return ""
	}
	answer, ok := q.Data["answer"]
	if !ok {
		return ""
	}
	if str, ok := answer.(string); ok {
		return str
	}
	return ""
}

func (v *Verifier) storedQuestionToQuestion(q models.StoredQuestion) Question {
	data := decodeQuestionData(q.Data)
	generator := q.Generator
	if generator == "" {
		generator, _ = data["generator"].(string)
	}
	questionType := q.Type
	if t, ok := data["type"].(string); ok && t != "" {
		questionType = t
	}
	prompt, _ := data["prompt"].(string)
	return Question{
		Generator: generator,
		Type:      questionType,
		Question:  prompt,
		Data:      data,
	}
}

func (v *Verifier) finalizeQuestion(q *Question, gen questionGenerator, qType string) {
	if q.Generator == "" {
		q.Generator = gen.name()
	}
	if q.Type == "" {
		q.Type = qType
	}
	if q.Data == nil {
		q.Data = map[string]interface{}{}
	}
	if q.Question == "" {
		if prompt, ok := q.Data["prompt"].(string); ok {
			q.Question = prompt
		}
	}
	if q.Question != "" {
		if _, ok := q.Data["prompt"]; !ok {
			q.Data["prompt"] = q.Question
		}
	}
	if _, ok := q.Data["generator"]; !ok && q.Generator != "" {
		q.Data["generator"] = q.Generator
	}
	if _, ok := q.Data["type"]; !ok && q.Type != "" {
		q.Data["type"] = q.Type
	}
}

func decodeQuestionData(payload string) map[string]interface{} {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return map[string]interface{}{}
	}
	data := map[string]interface{}{}
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		return map[string]interface{}{}
	}
	return data
}

func containsType(types []string, qType string) bool {
	for _, t := range types {
		if t == qType {
			return true
		}
	}
	return false
}

func findGenerator(generators []questionGenerator, name, qType string) questionGenerator {
	name = strings.ToLower(strings.TrimSpace(name))
	qType = strings.ToLower(strings.TrimSpace(qType))
	if name != "" {
		for _, gen := range generators {
			if gen.name() == name {
				return gen
			}
		}
	}
	if qType != "" {
		for _, gen := range generators {
			if containsType(gen.types(), qType) {
				return gen
			}
		}
	}
	return nil
}

func verifyByExpectedAnswer(question *Question, answer string) bool {
	if question == nil {
		return false
	}
	if question.Data == nil {
		return false
	}
	value, ok := question.Data["answer"]
	if !ok {
		return false
	}
	expected, ok := value.(string)
	if !ok {
		return false
	}
	return utils.NormalizeAnswer(answer) == utils.NormalizeAnswer(expected)
}

func repoFromQuestion(question *Question) string {
	if question == nil || question.Data == nil {
		return ""
	}
	value, ok := question.Data["repo"]
	if !ok {
		return ""
	}
	if repo, ok := value.(string); ok {
		return strings.TrimSpace(repo)
	}
	return ""
}
