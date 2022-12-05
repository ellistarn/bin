package main

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/ellistarn/bin/pkg/log"
	"github.com/ellistarn/slang/pkg/pretty"
	"github.com/samber/lo"
)

// A simple tool that shows how much time you've spent waiting for CFN
func main() {
	ctx := log.With(context.Background())
	blameCfn := BlameCfn{
		cfn: cloudformation.New(session.Must(session.NewSession())),
	}
	actions := blameCfn.Summarize(ctx)

	log.From(ctx).Info("Things that went well:")
	blameCfn.report(ctx, lo.Filter(actions, func(action Action, _ int) bool { return action.Success }))
	log.From(ctx).Info("Things that didn't go so well:")
	blameCfn.report(ctx, lo.Filter(actions, func(action Action, _ int) bool { return !action.Success }))
}

type BlameCfn struct {
	cfn *cloudformation.CloudFormation
}

func (b BlameCfn) Summarize(ctx context.Context) (actions []Action) {
	stacks := lo.Must(b.cfn.ListStacksWithContext(ctx, &cloudformation.ListStacksInput{})).StackSummaries
	for _, stack := range stacks {
		actions = append(actions, b.summarizeStack(ctx, stack)...)
	}
	sort.SliceStable(actions, func(i, j int) bool { return actions[i].Duration > actions[j].Duration })
	sort.SliceStable(actions, func(i, j int) bool { return !actions[i].Success })
	log.From(ctx).Infof("Found %d actions across %d stacks", len(actions), len(stacks))
	return actions
}

func (b BlameCfn) summarizeStack(ctx context.Context, stack *cloudformation.StackSummary) (actions []Action) {
	var events []*cloudformation.StackEvent
	var nextToken *string
	for {
		output := lo.Must(b.cfn.DescribeStackEventsWithContext(ctx, &cloudformation.DescribeStackEventsInput{StackName: stack.StackId, NextToken: nextToken}))
		events = append(events, output.StackEvents...)
		nextToken = output.NextToken
		if output.NextToken == nil {
			break
		}
	}
	events = lo.Filter(events, func(event *cloudformation.StackEvent, _ int) bool {
		return lo.FromPtr(event.LogicalResourceId) == lo.FromPtr(stack.StackName) &&
			// Nonterminal states
			lo.FromPtr(event.ResourceStatus) != "REVIEW_IN_PROGRESS" &&
			lo.FromPtr(event.ResourceStatus) != "UPDATE_COMPLETE_CLEANUP_IN_PROGRESS" &&
			lo.FromPtr(event.ResourceStatus) != "ROLLBACK_IN_PROGRESS" &&
			lo.FromPtr(event.ResourceStatus) != "UPDATE_ROLLBACK_IN_PROGRESS" &&
			lo.FromPtr(event.ResourceStatus) != "UPDATE_ROLLBACK_COMPLETE_CLEANUP_IN_PROGRESS"
	})

	sort.Slice(events, func(i, j int) bool { return events[i].Timestamp.Before(lo.FromPtr(events[j].Timestamp)) })

	for _, chunk := range lo.Chunk(events, 2) {
		if !lo.Contains([]string{
			"CREATE_IN_PROGRESS",
			"UPDATE_IN_PROGRESS",
			"DELETE_IN_PROGRESS",
		}, lo.FromPtr(chunk[0].ResourceStatus)) {
			log.From(ctx).Errorf("ignoring event that should have been user triggered, but wasn't\n%s", pretty.Verbose(chunk[0]))
			continue
		}
		if !lo.Contains([]string{
			"CREATE_COMPLETE",
			"UPDATE_COMPLETE",
			"DELETE_FAILED",
			"DELETE_COMPLETE",
			"ROLLBACK_COMPLETE",
			"UPDATE_ROLLBACK_COMPLETE",
		}, lo.FromPtr(chunk[1].ResourceStatus)) {
			log.From(ctx).Errorf("ignoring event that should have terminal, but wasn't\n%s", pretty.Verbose(chunk[1]))
			continue
		}

		actions = append(actions, Action{
			StackId:   lo.FromPtr(stack.StackId),
			Situation: lo.FromPtr(chunk[0].ResourceStatus),
			Outcome:   lo.FromPtr(chunk[1].ResourceStatus),
			Duration:  chunk[1].Timestamp.Sub(lo.FromPtr(chunk[0].Timestamp)),
			Success: lo.Contains([]string{
				"CREATE_COMPLETE",
				"UPDATE_COMPLETE",
				"DELETE_COMPLETE",
			}, lo.FromPtr(chunk[1].ResourceStatus)),
		})
	}
	return actions
}

func (b BlameCfn) report(ctx context.Context, actions []Action) {
	situationOutcomeActions := lo.GroupBy(actions, func(action Action) string { return fmt.Sprintf("%s -> %s", action.Situation, action.Outcome) })
	for situationOutcome, actions := range situationOutcomeActions {
		duration := lo.SumBy(actions, func(action Action) time.Duration { return action.Duration })
		log.From(ctx).Infof("%d x %s:\t%s (avg: %s)", len(actions), situationOutcome, duration, time.Duration(int(duration)/len(actions)))
	}
}

type Action struct {
	StackId   string
	Situation string
	Outcome   string
	Success   bool
	Duration  time.Duration
}
