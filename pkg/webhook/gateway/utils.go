package gateway

import (
	"context"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	v1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TenantFromIngress(ctx context.Context, c client.Client, gateway v1.ObjectName) (*capsulev1beta2.Tenant, error) {
	tenantList := &capsulev1beta2.TenantList{}
	objName := reflect.ValueOf(gateway).String()
	if err := c.List(ctx, tenantList, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector(".status.namespaces", objName),
	}); err != nil {
		return nil, err
	}

	if len(tenantList.Items) == 0 {
		return nil, nil //nolint:nilnil
	}

	return &tenantList.Items[0], nil
}

func GetGatewayClassClassByName(ctx context.Context, c client.Client, gatewayClassName v1.ObjectName) (*gatewayv1.GatewayClass, error) {
	objName := reflect.ValueOf(gatewayClassName).String()
	gatewayClass := &gatewayv1.GatewayClass{}

	if err := c.Get(ctx, types.NamespacedName{Name: objName}, gatewayClass); err != nil {
		return nil, err
	}
	return gatewayClass, nil
}
