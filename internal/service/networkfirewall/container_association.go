// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: MPL-2.0

package networkfirewall

import (
	"context"
	"fmt"
	"time"

	"github.com/YakDriver/regexache"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/networkfirewall"
	awstypes "github.com/aws/aws-sdk-go-v2/service/networkfirewall/types"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
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
	fwflex "github.com/hashicorp/terraform-provider-aws/internal/framework/flex"
	fwtypes "github.com/hashicorp/terraform-provider-aws/internal/framework/types"
	"github.com/hashicorp/terraform-provider-aws/internal/retry"
	tftags "github.com/hashicorp/terraform-provider-aws/internal/tags"
	"github.com/hashicorp/terraform-provider-aws/internal/tfresource"
	"github.com/hashicorp/terraform-provider-aws/names"
)

// @FrameworkResource("aws_networkfirewall_container_association", name="Container Association")
// @Tags(identifierAttribute="container_association_arn")
func newContainerAssociationResource(_ context.Context) (resource.ResourceWithConfigure, error) {
	r := &containerAssociationResource{}

	r.SetDefaultCreateTimeout(30 * time.Minute)
	r.SetDefaultUpdateTimeout(30 * time.Minute)
	r.SetDefaultDeleteTimeout(30 * time.Minute)

	return r, nil
}

type containerAssociationResource struct {
	framework.ResourceWithModel[containerAssociationResourceModel]
	framework.WithTimeouts
}

func (r *containerAssociationResource) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = schema.Schema{
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

func (r *containerAssociationResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var data containerAssociationResourceModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &data)...)
	if response.Diagnostics.HasError() {
		return
	}

	conn := r.Meta().NetworkFirewallClient(ctx)

	name := data.ContainerAssociationName.ValueString()

	configs, expandDiags := expandContainerMonitoringConfigurations(ctx, data.ContainerMonitoringConfigurations)
	response.Diagnostics.Append(expandDiags...)
	if response.Diagnostics.HasError() {
		return
	}

	input := networkfirewall.CreateContainerAssociationInput{
		ContainerAssociationName:          aws.String(name),
		ContainerMonitoringConfigurations: configs,
		Type:                              data.Type.ValueEnum(),
		Tags:                              getTagsIn(ctx),
	}
	if !data.Description.IsNull() && !data.Description.IsUnknown() {
		input.Description = data.Description.ValueStringPointer()
	}

	output, err := conn.CreateContainerAssociation(ctx, &input)
	if err != nil {
		response.Diagnostics.AddError(fmt.Sprintf("creating NetworkFirewall Container Association (%s)", name), err.Error())

		return
	}

	arn := aws.ToString(output.ContainerAssociationArn)
	data.ContainerAssociationARN = fwflex.StringToFramework(ctx, output.ContainerAssociationArn)
	data.Status = fwtypes.StringEnumValue(output.Status)
	data.UpdateToken = fwflex.StringToFramework(ctx, output.UpdateToken)

	outputD, err := waitContainerAssociationCreated(ctx, conn, arn, r.CreateTimeout(ctx, data.Timeouts))
	if err != nil {
		response.Diagnostics.AddError(fmt.Sprintf("waiting for NetworkFirewall Container Association (%s) create", arn), err.Error())

		return
	}

	data.Status = fwtypes.StringEnumValue(outputD.Status)
	if outputD.LastUpdatedTime != nil {
		data.LastUpdatedTime = fwflex.StringValueToFramework(ctx, outputD.LastUpdatedTime.Format(time.RFC3339))
	}
	data.ResolvedCIDRCount = fwflex.Int32ToFrameworkInt64(ctx, outputD.ResolvedCidrCount)

	response.Diagnostics.Append(response.State.Set(ctx, data)...)
}

func (r *containerAssociationResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
	var data containerAssociationResourceModel
	response.Diagnostics.Append(request.State.Get(ctx, &data)...)
	if response.Diagnostics.HasError() {
		return
	}

	conn := r.Meta().NetworkFirewallClient(ctx)

	arn := data.ContainerAssociationARN.ValueString()
	output, err := findContainerAssociationByARN(ctx, conn, arn)
	if retry.NotFound(err) {
		response.Diagnostics.Append(fwdiag.NewResourceNotFoundWarningDiagnostic(err))
		response.State.RemoveResource(ctx)

		return
	}

	if err != nil {
		response.Diagnostics.AddError(fmt.Sprintf("reading NetworkFirewall Container Association (%s)", arn), err.Error())

		return
	}

	data.ContainerAssociationARN = fwflex.StringToFramework(ctx, output.ContainerAssociationArn)
	data.ContainerAssociationName = fwflex.StringToFramework(ctx, output.ContainerAssociationName)
	data.Description = fwflex.StringToFramework(ctx, output.Description)
	data.Status = fwtypes.StringEnumValue(output.Status)
	data.Type = fwtypes.StringEnumValue(output.Type)
	data.UpdateToken = fwflex.StringToFramework(ctx, output.UpdateToken)
	if output.LastUpdatedTime != nil {
		data.LastUpdatedTime = fwflex.StringValueToFramework(ctx, output.LastUpdatedTime.Format(time.RFC3339))
	}
	data.ResolvedCIDRCount = fwflex.Int32ToFrameworkInt64(ctx, output.ResolvedCidrCount)

	configs, flattenDiags := flattenContainerMonitoringConfigurations(ctx, output.ContainerMonitoringConfigurations)
	response.Diagnostics.Append(flattenDiags...)
	if response.Diagnostics.HasError() {
		return
	}
	data.ContainerMonitoringConfigurations = configs

	setTagsOut(ctx, output.Tags)

	response.Diagnostics.Append(response.State.Set(ctx, &data)...)
}

func (r *containerAssociationResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	var old, new containerAssociationResourceModel
	response.Diagnostics.Append(request.State.Get(ctx, &old)...)
	if response.Diagnostics.HasError() {
		return
	}
	response.Diagnostics.Append(request.Plan.Get(ctx, &new)...)
	if response.Diagnostics.HasError() {
		return
	}

	conn := r.Meta().NetworkFirewallClient(ctx)

	if !new.ContainerMonitoringConfigurations.Equal(old.ContainerMonitoringConfigurations) ||
		!new.Description.Equal(old.Description) {
		arn := old.ContainerAssociationARN.ValueString()

		configs, expandDiags := expandContainerMonitoringConfigurations(ctx, new.ContainerMonitoringConfigurations)
		response.Diagnostics.Append(expandDiags...)
		if response.Diagnostics.HasError() {
			return
		}

		input := networkfirewall.UpdateContainerAssociationInput{
			ContainerAssociationArn:           aws.String(arn),
			ContainerMonitoringConfigurations: configs,
			Type:                              new.Type.ValueEnum(),
			UpdateToken:                       old.UpdateToken.ValueStringPointer(),
		}
		if !new.Description.IsNull() && !new.Description.IsUnknown() {
			input.Description = new.Description.ValueStringPointer()
		}

		output, err := conn.UpdateContainerAssociation(ctx, &input)
		if err != nil {
			response.Diagnostics.AddError(fmt.Sprintf("updating NetworkFirewall Container Association (%s)", arn), err.Error())

			return
		}

		new.UpdateToken = fwflex.StringToFramework(ctx, output.UpdateToken)

		outputD, err := waitContainerAssociationUpdated(ctx, conn, arn, r.UpdateTimeout(ctx, new.Timeouts))
		if err != nil {
			response.Diagnostics.AddError(fmt.Sprintf("waiting for NetworkFirewall Container Association (%s) update", arn), err.Error())

			return
		}

		new.Status = fwtypes.StringEnumValue(outputD.Status)
		if outputD.LastUpdatedTime != nil {
			new.LastUpdatedTime = fwflex.StringValueToFramework(ctx, outputD.LastUpdatedTime.Format(time.RFC3339))
		}
		new.ResolvedCIDRCount = fwflex.Int32ToFrameworkInt64(ctx, outputD.ResolvedCidrCount)
	} else {
		new.UpdateToken = old.UpdateToken
		new.LastUpdatedTime = old.LastUpdatedTime
		new.ResolvedCIDRCount = old.ResolvedCIDRCount
	}

	response.Diagnostics.Append(response.State.Set(ctx, &new)...)
}

func (r *containerAssociationResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
	var data containerAssociationResourceModel
	response.Diagnostics.Append(request.State.Get(ctx, &data)...)
	if response.Diagnostics.HasError() {
		return
	}

	conn := r.Meta().NetworkFirewallClient(ctx)

	arn := data.ContainerAssociationARN.ValueString()
	input := networkfirewall.DeleteContainerAssociationInput{
		ContainerAssociationArn: aws.String(arn),
	}
	_, err := conn.DeleteContainerAssociation(ctx, &input)
	if errs.IsA[*awstypes.ResourceNotFoundException](err) {
		return
	}

	if err != nil {
		response.Diagnostics.AddError(fmt.Sprintf("deleting NetworkFirewall Container Association (%s)", arn), err.Error())

		return
	}

	if _, err := waitContainerAssociationDeleted(ctx, conn, arn, r.DeleteTimeout(ctx, data.Timeouts)); err != nil {
		response.Diagnostics.AddError(fmt.Sprintf("waiting for NetworkFirewall Container Association (%s) delete", arn), err.Error())

		return
	}
}

func (r *containerAssociationResource) ImportState(ctx context.Context, request resource.ImportStateRequest, response *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("container_association_arn"), request, response)
}

func findContainerAssociation(ctx context.Context, conn *networkfirewall.Client, input *networkfirewall.DescribeContainerAssociationInput) (*networkfirewall.DescribeContainerAssociationOutput, error) {
	output, err := conn.DescribeContainerAssociation(ctx, input)

	if errs.IsA[*awstypes.ResourceNotFoundException](err) {
		return nil, &retry.NotFoundError{
			LastError: err,
		}
	}

	if err != nil {
		return nil, err
	}

	if output == nil {
		return nil, tfresource.NewEmptyResultError()
	}

	return output, nil
}

func findContainerAssociationByARN(ctx context.Context, conn *networkfirewall.Client, arn string) (*networkfirewall.DescribeContainerAssociationOutput, error) {
	input := networkfirewall.DescribeContainerAssociationInput{
		ContainerAssociationArn: aws.String(arn),
	}

	return findContainerAssociation(ctx, conn, &input)
}

func statusContainerAssociation(conn *networkfirewall.Client, arn string) retry.StateRefreshFunc {
	return func(ctx context.Context) (any, string, error) {
		output, err := findContainerAssociationByARN(ctx, conn, arn)

		if retry.NotFound(err) {
			return nil, "", nil
		}

		if err != nil {
			return nil, "", err
		}

		return output, string(output.Status), nil
	}
}

func waitContainerAssociationCreated(ctx context.Context, conn *networkfirewall.Client, arn string, timeout time.Duration) (*networkfirewall.DescribeContainerAssociationOutput, error) {
	stateConf := &retry.StateChangeConf{
		Pending:                   enum.Slice(awstypes.ContainerAssociationStatusCreating),
		Target:                    enum.Slice(awstypes.ContainerAssociationStatusActive),
		Refresh:                   statusContainerAssociation(conn, arn),
		Timeout:                   timeout,
		ContinuousTargetOccurence: 2,
	}

	outputRaw, err := stateConf.WaitForStateContext(ctx)

	if output, ok := outputRaw.(*networkfirewall.DescribeContainerAssociationOutput); ok {
		return output, err
	}

	return nil, err
}

func waitContainerAssociationUpdated(ctx context.Context, conn *networkfirewall.Client, arn string, timeout time.Duration) (*networkfirewall.DescribeContainerAssociationOutput, error) {
	stateConf := &retry.StateChangeConf{
		Pending:                   enum.Slice(awstypes.ContainerAssociationStatusCreating),
		Target:                    enum.Slice(awstypes.ContainerAssociationStatusActive),
		Refresh:                   statusContainerAssociation(conn, arn),
		Timeout:                   timeout,
		ContinuousTargetOccurence: 2,
	}

	outputRaw, err := stateConf.WaitForStateContext(ctx)

	if output, ok := outputRaw.(*networkfirewall.DescribeContainerAssociationOutput); ok {
		return output, err
	}

	return nil, err
}

func waitContainerAssociationDeleted(ctx context.Context, conn *networkfirewall.Client, arn string, timeout time.Duration) (*networkfirewall.DescribeContainerAssociationOutput, error) {
	stateConf := &retry.StateChangeConf{
		Pending: enum.Slice(awstypes.ContainerAssociationStatusActive, awstypes.ContainerAssociationStatusDeleting),
		Target:  []string{},
		Refresh: statusContainerAssociation(conn, arn),
		Timeout: timeout,
	}

	outputRaw, err := stateConf.WaitForStateContext(ctx)

	if output, ok := outputRaw.(*networkfirewall.DescribeContainerAssociationOutput); ok {
		return output, err
	}

	return nil, err
}

func expandContainerMonitoringConfigurations(ctx context.Context, tfList fwtypes.ListNestedObjectValueOf[containerMonitoringConfigurationModel]) ([]awstypes.ContainerMonitoringConfiguration, diag.Diagnostics) { // nosemgrep:ci.semgrep.framework.manual-flattener-functions
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

func flattenContainerMonitoringConfigurations(ctx context.Context, apiList []awstypes.ContainerMonitoringConfiguration) (fwtypes.ListNestedObjectValueOf[containerMonitoringConfigurationModel], diag.Diagnostics) { // nosemgrep:ci.semgrep.framework.manual-flattener-functions
	var diags diag.Diagnostics

	objs := make([]*containerMonitoringConfigurationModel, 0, len(apiList))
	for _, cfg := range apiList {
		m := &containerMonitoringConfigurationModel{
			ClusterARN: fwtypes.ARNValue(aws.ToString(cfg.ClusterArn)),
		}

		attrs := make([]*containerAttributeModel, 0, len(cfg.AttributeFilters))
		for _, attr := range cfg.AttributeFilters {
			attrs = append(attrs, &containerAttributeModel{
				Key:   fwflex.StringToFramework(ctx, attr.Key),
				Value: fwflex.StringToFramework(ctx, attr.Value),
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
