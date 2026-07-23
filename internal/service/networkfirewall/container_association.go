// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: MPL-2.0

// DONOTCOPY: Copying old resources spreads bad habits. Use skaff instead.

package networkfirewall

import (
	"context"
	"fmt"
	"time"

	"github.com/YakDriver/regexache"
	"github.com/YakDriver/smarterr"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/networkfirewall"
	awstypes "github.com/aws/aws-sdk-go-v2/service/networkfirewall/types"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-provider-aws/internal/enum"
	"github.com/hashicorp/terraform-provider-aws/internal/errs"
	"github.com/hashicorp/terraform-provider-aws/internal/errs/fwdiag"
	"github.com/hashicorp/terraform-provider-aws/internal/framework"
	"github.com/hashicorp/terraform-provider-aws/internal/framework/flex"
	fwtypes "github.com/hashicorp/terraform-provider-aws/internal/framework/types"
	"github.com/hashicorp/terraform-provider-aws/internal/retry"
	"github.com/hashicorp/terraform-provider-aws/internal/smerr"
	tftags "github.com/hashicorp/terraform-provider-aws/internal/tags"
	"github.com/hashicorp/terraform-provider-aws/internal/tfresource"
	"github.com/hashicorp/terraform-provider-aws/names"
)

// @FrameworkResource("aws_networkfirewall_container_association", name="Container Association")
// @Tags(identifierAttribute="container_association_arn")
// @Testing(existsType="github.com/aws/aws-sdk-go-v2/service/networkfirewall;networkfirewall.DescribeContainerAssociationOutput")
// @Testing(importIgnore="update_token")
func newContainerAssociationResource(_ context.Context) (resource.ResourceWithConfigure, error) {
	r := &containerAssociationResource{}

	r.SetDefaultCreateTimeout(30 * time.Minute)
	r.SetDefaultUpdateTimeout(30 * time.Minute)
	r.SetDefaultDeleteTimeout(30 * time.Minute)

	return r, nil
}

const (
	ResNameContainerAssociation = "Container Association"
)

type containerAssociationResource struct {
	framework.ResourceWithModel[containerAssociationResourceModel]
	framework.WithTimeouts
}

func (r *containerAssociationResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"container_association_arn": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"container_association_name": schema.StringAttribute{
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 128),
					stringvalidator.RegexMatches(
						regexache.MustCompile(`^[a-zA-Z0-9-]+$`),
						"Must contain only alphanumeric characters and hyphens",
					),
				},
			},
			names.AttrDescription: schema.StringAttribute{
				Optional: true,
			},
			"last_updated_time": schema.StringAttribute{
				Computed: true,
			},
			"resolved_cidr_count": schema.Int64Attribute{
				Computed: true,
			},
			names.AttrStatus: schema.StringAttribute{
				CustomType: fwtypes.StringEnumType[awstypes.ContainerAssociationStatus](),
				Computed:   true,
			},
			names.AttrTags:    tftags.TagsAttribute(),
			names.AttrTagsAll: tftags.TagsAttributeComputedOnly(),
			names.AttrType: schema.StringAttribute{
				CustomType: fwtypes.StringEnumType[awstypes.ContainerMonitoringType](),
				Required:   true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"update_token": schema.StringAttribute{
				Computed: true,
			},
		},
		Blocks: map[string]schema.Block{
			"container_monitoring_configurations": schema.ListNestedBlock{
				CustomType: fwtypes.NewListNestedObjectTypeOf[containerMonitoringConfigurationModel](ctx),
				Validators: []validator.List{
					listvalidator.IsRequired(),
					listvalidator.SizeAtLeast(1),
				},
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"cluster_arn": schema.StringAttribute{
							CustomType: fwtypes.ARNType,
							Required:   true,
						},
					},
					Blocks: map[string]schema.Block{
						"attribute_filters": schema.ListNestedBlock{
							CustomType: fwtypes.NewListNestedObjectTypeOf[containerAttributeModel](ctx),
							NestedObject: schema.NestedBlockObject{
								Attributes: map[string]schema.Attribute{
									names.AttrKey: schema.StringAttribute{
										Required: true,
									},
									names.AttrValue: schema.StringAttribute{
										Required: true,
									},
								},
							},
						},
					},
				},
			},
			names.AttrTimeouts: timeouts.Block(ctx, timeouts.Opts{
				Create: true,
				Update: true,
				Delete: true,
			}),
		},
	}
}

func (r *containerAssociationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	conn := r.Meta().NetworkFirewallClient(ctx)

	var plan containerAssociationResourceModel
	smerr.AddEnrich(ctx, &resp.Diagnostics, req.Plan.Get(ctx, &plan))
	if resp.Diagnostics.HasError() {
		return
	}

	configs, d := expandContainerMonitoringConfigurations(ctx, plan.ContainerMonitoringConfigurations)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := networkfirewall.CreateContainerAssociationInput{
		ContainerAssociationName:          plan.ContainerAssociationName.ValueStringPointer(),
		ContainerMonitoringConfigurations: configs,
		Type:                              plan.Type.ValueEnum(),
		Tags:                              getTagsIn(ctx),
	}
	if !plan.Description.IsNull() {
		input.Description = plan.Description.ValueStringPointer()
	}

	out, err := conn.CreateContainerAssociation(ctx, &input)
	if err != nil {
		smerr.AddError(ctx, &resp.Diagnostics, err, smerr.ID, plan.ContainerAssociationName.String())
		return
	}

	arn := aws.ToString(out.ContainerAssociationArn)
	plan.ContainerAssociationARN = flex.StringToFramework(ctx, out.ContainerAssociationArn)
	plan.Status = fwtypes.StringEnumValue(out.Status)
	plan.UpdateToken = flex.StringToFramework(ctx, out.UpdateToken)

	createTimeout := r.CreateTimeout(ctx, plan.Timeouts)
	outD, err := waitContainerAssociationCreated(ctx, conn, arn, createTimeout)
	if err != nil {
		smerr.AddError(ctx, &resp.Diagnostics, err, smerr.ID, arn)
		return
	}

	plan.Status = fwtypes.StringEnumValue(outD.Status)
	if outD.LastUpdatedTime != nil {
		plan.LastUpdatedTime = flex.StringValueToFramework(ctx, outD.LastUpdatedTime.Format(time.RFC3339))
	}
	plan.ResolvedCIDRCount = flex.Int32ToFrameworkInt64(ctx, outD.ResolvedCidrCount)

	smerr.AddEnrich(ctx, &resp.Diagnostics, resp.State.Set(ctx, plan))
}

func (r *containerAssociationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	conn := r.Meta().NetworkFirewallClient(ctx)

	var state containerAssociationResourceModel
	smerr.AddEnrich(ctx, &resp.Diagnostics, req.State.Get(ctx, &state))
	if resp.Diagnostics.HasError() {
		return
	}

	arn := state.ContainerAssociationARN.ValueString()
	out, err := findContainerAssociationByARN(ctx, conn, arn)
	if retry.NotFound(err) {
		resp.Diagnostics.Append(fwdiag.NewResourceNotFoundWarningDiagnostic(err))
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		smerr.AddError(ctx, &resp.Diagnostics, err, smerr.ID, arn)
		return
	}

	state.ContainerAssociationARN = flex.StringToFramework(ctx, out.ContainerAssociationArn)
	state.ContainerAssociationName = flex.StringToFramework(ctx, out.ContainerAssociationName)
	state.Description = flex.StringToFramework(ctx, out.Description)
	state.Status = fwtypes.StringEnumValue(out.Status)
	state.Type = fwtypes.StringEnumValue(out.Type)
	state.UpdateToken = flex.StringToFramework(ctx, out.UpdateToken)
	if out.LastUpdatedTime != nil {
		state.LastUpdatedTime = flex.StringValueToFramework(ctx, out.LastUpdatedTime.Format(time.RFC3339))
	}
	state.ResolvedCIDRCount = flex.Int32ToFrameworkInt64(ctx, out.ResolvedCidrCount)

	configs, d := flattenContainerMonitoringConfigurations(ctx, out.ContainerMonitoringConfigurations)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.ContainerMonitoringConfigurations = configs

	setTagsOut(ctx, out.Tags)

	smerr.AddEnrich(ctx, &resp.Diagnostics, resp.State.Set(ctx, &state))
}

func (r *containerAssociationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	conn := r.Meta().NetworkFirewallClient(ctx)

	var plan, state containerAssociationResourceModel
	smerr.AddEnrich(ctx, &resp.Diagnostics, req.Plan.Get(ctx, &plan))
	smerr.AddEnrich(ctx, &resp.Diagnostics, req.State.Get(ctx, &state))
	if resp.Diagnostics.HasError() {
		return
	}

	diff, d := flex.Diff(ctx, plan, state)
	smerr.AddEnrich(ctx, &resp.Diagnostics, d)
	if resp.Diagnostics.HasError() {
		return
	}

	if diff.HasChanges() {
		arn := state.ContainerAssociationARN.ValueString()

		configs, d := expandContainerMonitoringConfigurations(ctx, plan.ContainerMonitoringConfigurations)
		resp.Diagnostics.Append(d...)
		if resp.Diagnostics.HasError() {
			return
		}

		input := networkfirewall.UpdateContainerAssociationInput{
			ContainerAssociationArn:           aws.String(arn),
			ContainerMonitoringConfigurations: configs,
			Type:                              plan.Type.ValueEnum(),
			UpdateToken:                       state.UpdateToken.ValueStringPointer(),
		}
		if !plan.Description.IsNull() {
			input.Description = plan.Description.ValueStringPointer()
		}

		out, err := conn.UpdateContainerAssociation(ctx, &input)
		if err != nil {
			smerr.AddError(ctx, &resp.Diagnostics, err, smerr.ID, arn)
			return
		}

		plan.UpdateToken = flex.StringToFramework(ctx, out.UpdateToken)

		updateTimeout := r.UpdateTimeout(ctx, plan.Timeouts)
		outD, err := waitContainerAssociationUpdated(ctx, conn, arn, updateTimeout)
		if err != nil {
			smerr.AddError(ctx, &resp.Diagnostics, err, smerr.ID, arn)
			return
		}

		plan.Status = fwtypes.StringEnumValue(outD.Status)
		if outD.LastUpdatedTime != nil {
			plan.LastUpdatedTime = flex.StringValueToFramework(ctx, outD.LastUpdatedTime.Format(time.RFC3339))
		}
		plan.ResolvedCIDRCount = flex.Int32ToFrameworkInt64(ctx, outD.ResolvedCidrCount)
	}

	smerr.AddEnrich(ctx, &resp.Diagnostics, resp.State.Set(ctx, &plan))
}

func (r *containerAssociationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	conn := r.Meta().NetworkFirewallClient(ctx)

	var state containerAssociationResourceModel
	smerr.AddEnrich(ctx, &resp.Diagnostics, req.State.Get(ctx, &state))
	if resp.Diagnostics.HasError() {
		return
	}

	arn := state.ContainerAssociationARN.ValueString()
	input := networkfirewall.DeleteContainerAssociationInput{
		ContainerAssociationArn: aws.String(arn),
	}

	_, err := conn.DeleteContainerAssociation(ctx, &input)
	if err != nil {
		if errs.IsA[*awstypes.ResourceNotFoundException](err) {
			return
		}
		smerr.AddError(ctx, &resp.Diagnostics, err, smerr.ID, arn)
		return
	}

	deleteTimeout := r.DeleteTimeout(ctx, state.Timeouts)
	_, err = waitContainerAssociationDeleted(ctx, conn, arn, deleteTimeout)
	if err != nil {
		smerr.AddError(ctx, &resp.Diagnostics, err, smerr.ID, arn)
		return
	}
}

func (r *containerAssociationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import by ARN.
	out, err := findContainerAssociationByARN(ctx, r.Meta().NetworkFirewallClient(ctx), req.ID)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("importing NetworkFirewall Container Association (%s)", req.ID), err.Error())
		return
	}

	var state containerAssociationResourceModel
	state.ContainerAssociationARN = flex.StringToFramework(ctx, out.ContainerAssociationArn)
	state.ContainerAssociationName = flex.StringToFramework(ctx, out.ContainerAssociationName)
	state.Description = flex.StringToFramework(ctx, out.Description)
	state.Status = fwtypes.StringEnumValue(out.Status)
	state.Type = fwtypes.StringEnumValue(out.Type)
	state.UpdateToken = flex.StringToFramework(ctx, out.UpdateToken)
	if out.LastUpdatedTime != nil {
		state.LastUpdatedTime = flex.StringValueToFramework(ctx, out.LastUpdatedTime.Format(time.RFC3339))
	}
	state.ResolvedCIDRCount = flex.Int32ToFrameworkInt64(ctx, out.ResolvedCidrCount)

	configs, d := flattenContainerMonitoringConfigurations(ctx, out.ContainerMonitoringConfigurations)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.ContainerMonitoringConfigurations = configs

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func waitContainerAssociationCreated(ctx context.Context, conn *networkfirewall.Client, arn string, timeout time.Duration) (*networkfirewall.DescribeContainerAssociationOutput, error) {
	stateConf := &retry.StateChangeConf{
		Pending:                   enum.Slice(awstypes.ContainerAssociationStatusCreating),
		Target:                    enum.Slice(awstypes.ContainerAssociationStatusActive),
		Refresh:                   statusContainerAssociation(conn, arn),
		Timeout:                   timeout,
		NotFoundChecks:            20,
		ContinuousTargetOccurence: 2,
	}

	outputRaw, err := stateConf.WaitForStateContext(ctx)
	if out, ok := outputRaw.(*networkfirewall.DescribeContainerAssociationOutput); ok {
		return out, smarterr.NewError(err)
	}

	return nil, smarterr.NewError(err)
}

func waitContainerAssociationUpdated(ctx context.Context, conn *networkfirewall.Client, arn string, timeout time.Duration) (*networkfirewall.DescribeContainerAssociationOutput, error) {
	stateConf := &retry.StateChangeConf{
		Pending:                   enum.Slice(awstypes.ContainerAssociationStatusCreating),
		Target:                    enum.Slice(awstypes.ContainerAssociationStatusActive),
		Refresh:                   statusContainerAssociation(conn, arn),
		Timeout:                   timeout,
		NotFoundChecks:            20,
		ContinuousTargetOccurence: 2,
	}

	outputRaw, err := stateConf.WaitForStateContext(ctx)
	if out, ok := outputRaw.(*networkfirewall.DescribeContainerAssociationOutput); ok {
		return out, smarterr.NewError(err)
	}

	return nil, smarterr.NewError(err)
}

func waitContainerAssociationDeleted(ctx context.Context, conn *networkfirewall.Client, arn string, timeout time.Duration) (*networkfirewall.DescribeContainerAssociationOutput, error) {
	stateConf := &retry.StateChangeConf{
		Pending: enum.Slice(awstypes.ContainerAssociationStatusActive, awstypes.ContainerAssociationStatusDeleting),
		Target:  []string{},
		Refresh: statusContainerAssociation(conn, arn),
		Timeout: timeout,
	}

	outputRaw, err := stateConf.WaitForStateContext(ctx)
	if out, ok := outputRaw.(*networkfirewall.DescribeContainerAssociationOutput); ok {
		return out, smarterr.NewError(err)
	}

	return nil, smarterr.NewError(err)
}

func statusContainerAssociation(conn *networkfirewall.Client, arn string) retry.StateRefreshFunc {
	return func(ctx context.Context) (any, string, error) {
		out, err := findContainerAssociationByARN(ctx, conn, arn)
		if retry.NotFound(err) {
			return nil, "", nil
		}
		if err != nil {
			return nil, "", smarterr.NewError(err)
		}

		return out, string(out.Status), nil
	}
}

func findContainerAssociationByARN(ctx context.Context, conn *networkfirewall.Client, arn string) (*networkfirewall.DescribeContainerAssociationOutput, error) {
	input := networkfirewall.DescribeContainerAssociationInput{
		ContainerAssociationArn: aws.String(arn),
	}

	out, err := conn.DescribeContainerAssociation(ctx, &input)
	if err != nil {
		if errs.IsA[*awstypes.ResourceNotFoundException](err) {
			return nil, smarterr.NewError(&retry.NotFoundError{
				LastError: err,
			})
		}
		return nil, smarterr.NewError(err)
	}

	if out == nil {
		return nil, smarterr.NewError(tfresource.NewEmptyResultError())
	}

	return out, nil
}

func expandContainerMonitoringConfigurations(ctx context.Context, tfList fwtypes.ListNestedObjectValueOf[containerMonitoringConfigurationModel]) ([]awstypes.ContainerMonitoringConfiguration, diag.Diagnostics) {
	var diags diag.Diagnostics

	objs, d := tfList.ToSlice(ctx)
	diags.Append(d...)
	if diags.HasError() {
		return nil, diags
	}

	result := make([]awstypes.ContainerMonitoringConfiguration, 0, len(objs))
	for _, obj := range objs {
		cfg := awstypes.ContainerMonitoringConfiguration{
			ClusterArn: obj.ClusterARN.ValueStringPointer(),
		}

		attrObjs, d := obj.AttributeFilters.ToSlice(ctx)
		diags.Append(d...)
		if diags.HasError() {
			return nil, diags
		}
		for _, attr := range attrObjs {
			cfg.AttributeFilters = append(cfg.AttributeFilters, awstypes.ContainerAttribute{
				Key:   attr.Key.ValueStringPointer(),
				Value: attr.Value.ValueStringPointer(),
			})
		}

		result = append(result, cfg)
	}

	return result, diags
}

func flattenContainerMonitoringConfigurations(ctx context.Context, apiList []awstypes.ContainerMonitoringConfiguration) (fwtypes.ListNestedObjectValueOf[containerMonitoringConfigurationModel], diag.Diagnostics) {
	var diags diag.Diagnostics

	objs := make([]*containerMonitoringConfigurationModel, 0, len(apiList))
	for _, cfg := range apiList {
		m := &containerMonitoringConfigurationModel{
			ClusterARN: fwtypes.ARNValue(aws.ToString(cfg.ClusterArn)),
		}

		attrs := make([]*containerAttributeModel, 0, len(cfg.AttributeFilters))
		for _, attr := range cfg.AttributeFilters {
			attrs = append(attrs, &containerAttributeModel{
				Key:   flex.StringToFramework(ctx, attr.Key),
				Value: flex.StringToFramework(ctx, attr.Value),
			})
		}

		attrList, d := fwtypes.NewListNestedObjectValueOfSlice(ctx, attrs, nil)
		diags.Append(d...)
		if diags.HasError() {
			return fwtypes.NewListNestedObjectValueOfNull[containerMonitoringConfigurationModel](ctx), diags
		}
		m.AttributeFilters = attrList

		objs = append(objs, m)
	}

	result, d := fwtypes.NewListNestedObjectValueOfSlice(ctx, objs, nil)
	diags.Append(d...)
	if diags.HasError() {
		return fwtypes.NewListNestedObjectValueOfNull[containerMonitoringConfigurationModel](ctx), diags
	}

	return result, diags
}

type containerAssociationResourceModel struct {
	framework.WithRegionModel
	ContainerAssociationARN           types.String                                                           `tfsdk:"container_association_arn"`
	ContainerAssociationName          types.String                                                           `tfsdk:"container_association_name"`
	ContainerMonitoringConfigurations fwtypes.ListNestedObjectValueOf[containerMonitoringConfigurationModel] `tfsdk:"container_monitoring_configurations"`
	Description                       types.String                                                           `tfsdk:"description"`
	LastUpdatedTime                   types.String                                                           `tfsdk:"last_updated_time"`
	ResolvedCIDRCount                 types.Int64                                                            `tfsdk:"resolved_cidr_count"`
	Status                            fwtypes.StringEnum[awstypes.ContainerAssociationStatus]                `tfsdk:"status"`
	Tags                              tftags.Map                                                             `tfsdk:"tags"`
	TagsAll                           tftags.Map                                                             `tfsdk:"tags_all"`
	Timeouts                          timeouts.Value                                                         `tfsdk:"timeouts"`
	Type                              fwtypes.StringEnum[awstypes.ContainerMonitoringType]                   `tfsdk:"type"`
	UpdateToken                       types.String                                                           `tfsdk:"update_token"`
}

type containerMonitoringConfigurationModel struct {
	AttributeFilters fwtypes.ListNestedObjectValueOf[containerAttributeModel] `tfsdk:"attribute_filters"`
	ClusterARN       fwtypes.ARN                                              `tfsdk:"cluster_arn"`
}

type containerAttributeModel struct {
	Key   types.String `tfsdk:"key"`
	Value types.String `tfsdk:"value"`
}
