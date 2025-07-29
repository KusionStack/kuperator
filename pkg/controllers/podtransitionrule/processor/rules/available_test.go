package rules

import (
	"testing"

	"k8s.io/utils/ptr"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
)

func TestExpFuncCal(t *testing.T) {
	expFunc := &appsv1alpha1.ExpFunc{
		Coeff: ptr.To("1.0"),
		Pow:   ptr.To("0.7"),
	}

	if val, _, _, _ := getValueFromExponentiation(expFunc, 1, false); val != 1 {
		t.Fatalf("unexpected %d", val)
	}

	if val, _, _, _ := getValueFromExponentiation(expFunc, 2, false); val != 1 {
		t.Fatalf("unexpected %d", val)
	}

	if val, _, _, _ := getValueFromExponentiation(expFunc, 10, false); val != 5 {
		t.Fatalf("unexpected %d", val)
	}

	if val, _, _, _ := getValueFromExponentiation(expFunc, 50, false); val != 15 {
		t.Fatalf("unexpected %d", val)
	}

	if val, _, _, _ := getValueFromExponentiation(expFunc, 500, false); val != 77 {
		t.Fatalf("unexpected %d", val)
	}

	if val, _, _, _ := getValueFromExponentiation(expFunc, 1000, false); val != 125 {
		t.Fatalf("unexpected %d", val)
	}
}

func TestExpFuncCalException(t *testing.T) {
	expFunc := &appsv1alpha1.ExpFunc{
		Coeff: ptr.To("2.0"),
		Pow:   ptr.To("0.7"),
	}

	if val, _, _, _ := getValueFromExponentiation(expFunc, 10, false); val != 10 {
		t.Fatalf("unexpected %d", val)
	}

	expFunc = &appsv1alpha1.ExpFunc{
		Coeff: ptr.To("0.0"),
		Pow:   ptr.To("0.7"),
	}

	if val, _, _, _ := getValueFromExponentiation(expFunc, 10, false); val != 0 {
		t.Fatalf("unexpected %d", val)
	}

	expFunc = &appsv1alpha1.ExpFunc{
		Coeff: ptr.To("0.0"),
		Pow:   ptr.To("0.0"),
	}

	if val, _, _, _ := getValueFromExponentiation(expFunc, 10, false); val != 0 {
		t.Fatalf("unexpected %d", val)
	}

	expFunc = &appsv1alpha1.ExpFunc{
		Coeff: ptr.To("1.0"),
		Pow:   ptr.To("0.0"),
	}

	if val, _, _, _ := getValueFromExponentiation(expFunc, 10, false); val != 1 {
		t.Fatalf("unexpected %d", val)
	}

	expFunc = &appsv1alpha1.ExpFunc{
		Coeff: ptr.To("-1.0"),
		Pow:   ptr.To("-0.7"),
	}

	if val, _, _, _ := getValueFromExponentiation(expFunc, 1, false); val != 0 {
		t.Fatalf("unexpected %d", val)
	}

	expFunc = &appsv1alpha1.ExpFunc{
		Coeff: nil,
		Pow:   nil,
	}

	if val, _, _, _ := getValueFromExponentiation(expFunc, 10, false); val != 5 {
		t.Fatalf("unexpected %d", val)
	}
}
