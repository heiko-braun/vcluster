package syncer

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/loft-sh/vcluster/pkg/mappings"
	"github.com/loft-sh/vcluster/pkg/scheme"
	"github.com/loft-sh/vcluster/pkg/syncer/synccontext"
	syncertesting "github.com/loft-sh/vcluster/pkg/syncer/testing"
	"github.com/loft-sh/vcluster/pkg/syncer/translator"
	syncertypes "github.com/loft-sh/vcluster/pkg/syncer/types"
	"github.com/loft-sh/vcluster/pkg/util/loghelper"
	testingutil "github.com/loft-sh/vcluster/pkg/util/testing"
	"github.com/loft-sh/vcluster/pkg/util/translate"
	"github.com/moby/locker"
	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// named mock instead of fake because there's a real "fake" syncer that syncs fake objects
type mockSyncer struct {
	syncertypes.GenericTranslator
}

func NewMockSyncer(ctx *synccontext.RegisterContext) (syncertypes.Object, error) {
	mapper, err := ctx.Mappings.ByGVK(mappings.Secrets())
	if err != nil {
		return nil, err
	}

	return &mockSyncer{
		GenericTranslator: translator.NewGenericTranslator(ctx, "secrets", &corev1.Secret{}, mapper),
	}, nil
}

func (s *mockSyncer) naiveTranslateCreate(ctx *synccontext.SyncContext, vObj client.Object) client.Object {
	pObj := translate.HostMetadata(ctx, vObj, s.VirtualToHost(ctx, types.NamespacedName{Name: vObj.GetName(), Namespace: vObj.GetNamespace()}, vObj))
	return pObj
}

func (s *mockSyncer) Syncer() syncertypes.Sync[client.Object] {
	return ToGenericSyncer[*corev1.Secret](s)
}

// SyncToHost is called when a virtual object was created and needs to be synced down to the physical cluster
func (s *mockSyncer) SyncToHost(ctx *synccontext.SyncContext, event *synccontext.SyncToHostEvent[*corev1.Secret]) (ctrl.Result, error) {
	pObj := s.naiveTranslateCreate(ctx, event.Virtual)
	if pObj == nil {
		return ctrl.Result{}, errors.New("naive translate create failed")
	}

	return CreateHostObject(ctx, event.Virtual, pObj, s.EventRecorder())
}

// Sync is called to sync a virtual object with a physical object
func (s *mockSyncer) Sync(ctx *synccontext.SyncContext, event *synccontext.SyncEvent[*corev1.Secret]) (ctrl.Result, error) {
	newPObj := event.Host.DeepCopyObject().(client.Object)
	newPObj.SetAnnotations(translate.HostAnnotations(event.Virtual, event.Host))
	newPObj.SetLabels(translate.HostLabels(ctx, event.Virtual, event.Host))
	return ctrl.Result{}, ctx.VirtualClient.Update(ctx, newPObj)
}

func (s *mockSyncer) SyncToVirtual(ctx *synccontext.SyncContext, event *synccontext.SyncToVirtualEvent[*corev1.Secret]) (_ ctrl.Result, retErr error) {
	// virtual object is not here anymore, so we delete
	return DeleteHostObject(ctx, event.Host, "virtual object was deleted")
}

var _ syncertypes.Syncer = &mockSyncer{}

var (
	vclusterNamespace    = "test"
	namespaceInVclusterA = "default"
)

func TestReconcile(t *testing.T) {
	translator := translate.NewSingleNamespaceTranslator(vclusterNamespace)

	type testCase struct {
		Name  string
		Focus bool

		Syncer func(ctx *synccontext.RegisterContext) (syncertypes.Object, error)

		EnqueueObjs []types.NamespacedName

		InitialPhysicalState []runtime.Object
		InitialVirtualState  []runtime.Object

		ExpectedPhysicalState map[schema.GroupVersionKind][]runtime.Object
		ExpectedVirtualState  map[schema.GroupVersionKind][]runtime.Object

		Compare syncertesting.Compare

		shouldErr bool
		errMsg    string
	}

	testCases := []testCase{
		{
			Name:   "should sync down",
			Syncer: NewMockSyncer,

			EnqueueObjs: []types.NamespacedName{
				{Name: "a", Namespace: namespaceInVclusterA},
			},

			InitialVirtualState: []runtime.Object{
				// secret that might be created by ingress controller or cert managers
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "a",
						Namespace: namespaceInVclusterA,
						UID:       "123",
					},
				},
			},

			InitialPhysicalState: []runtime.Object{
				// secret that might be created by ingress controller or cert managers
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "a",
						Namespace: vclusterNamespace,
						UID:       "123",
					},
				},
			},

			ExpectedVirtualState: map[schema.GroupVersionKind][]runtime.Object{
				// existing secret should remain
				corev1.SchemeGroupVersion.WithKind("Secret"): {
					&corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "a",
							Namespace: namespaceInVclusterA,
							UID:       "123",
						},
					},
				},
			},

			ExpectedPhysicalState: map[schema.GroupVersionKind][]runtime.Object{
				// existing secret should remain
				corev1.SchemeGroupVersion.WithKind("Secret"): {
					&corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "a",
							Namespace: vclusterNamespace,
							UID:       "123",
						},
					},
					&corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      translator.HostName("a", namespaceInVclusterA),
							Namespace: vclusterNamespace,
							Annotations: map[string]string{
								translate.NameAnnotation:      "a",
								translate.NamespaceAnnotation: namespaceInVclusterA,
								translate.UIDAnnotation:       "123",
								translate.KindAnnotation:      corev1.SchemeGroupVersion.WithKind("Secret").String(),
							},
							Labels: map[string]string{
								translate.NamespaceLabel: namespaceInVclusterA,
							},
						},
					},
				},
			},

			shouldErr: false,
		},
		{
			Name:   "should fail to sync down when object of desired name already exists",
			Syncer: NewMockSyncer,

			EnqueueObjs: []types.NamespacedName{
				{Name: "a", Namespace: namespaceInVclusterA},
			},

			InitialVirtualState: []runtime.Object{
				// secret that might be created by ingress controller or cert managers
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "a",
						Namespace: namespaceInVclusterA,
						UID:       "123",
					},
				},
			},

			InitialPhysicalState: []runtime.Object{
				// existing object doesn't have annotations/labels indicating it is owned, but has the name of the synced object
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      translate.Default.HostName("a", namespaceInVclusterA),
						Namespace: vclusterNamespace,
						Annotations: map[string]string{
							"app": "existing",
						},
						Labels: map[string]string{
							"app": "existing",
						},
					},
					Data: map[string][]byte{
						"datakey1": []byte("datavalue1"),
					},
				},
			},

			ExpectedVirtualState: map[schema.GroupVersionKind][]runtime.Object{
				// existing secret should remain
				corev1.SchemeGroupVersion.WithKind("Secret"): {
					&corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "a",
							Namespace: namespaceInVclusterA,
							UID:       "123",
						},
					},
				},
			},

			ExpectedPhysicalState: map[schema.GroupVersionKind][]runtime.Object{
				// existing secret should remain
				corev1.SchemeGroupVersion.WithKind("Secret"): {
					&corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      translator.HostName("a", namespaceInVclusterA),
							Namespace: vclusterNamespace,
							Annotations: map[string]string{
								"app": "existing",
							},
							Labels: map[string]string{
								"app": "existing",
							},
						},
						Data: map[string][]byte{
							"datakey1": []byte("datavalue1"),
						},
					},
				},
			},

			shouldErr: true,
			errMsg:    "conflict: cannot sync virtual object default/a as unmanaged physical object test/a-x-default-x-suffix exists with desired name",
		},
	}
	sort.SliceStable(testCases, func(i, j int) bool {
		// place focused tests first
		return testCases[i].Focus && !testCases[j].Focus
	})
	// record if any tests were focused
	hasFocus := false
	for i, tc := range testCases {
		t.Logf("running test #%d: %s", i, tc.Name)
		if tc.Focus {
			hasFocus = true
			t.Log("test is focused")
		} else if hasFocus { // fail if any tests were focused
			t.Fatal("some tests are focused")
		}

		// testing scenario:
		// virt object queued (existing, nonexisting)
		// corresponding physical object (nil, not-nil)

		// setup mocks
		options := &syncertypes.Options{}
		ctx := context.Background()
		pClient := testingutil.NewFakeClient(scheme.Scheme, tc.InitialPhysicalState...)
		vClient := testingutil.NewFakeClient(scheme.Scheme, tc.InitialVirtualState...)

		fakeContext := syncertesting.NewFakeRegisterContext(syncertesting.NewFakeConfig(), pClient, vClient)
		syncerImpl, err := tc.Syncer(fakeContext)
		assert.NilError(t, err)
		syncer := syncerImpl.(syncertypes.Syncer)

		controller := &SyncController{
			syncer: syncer,

			genericSyncer: syncer.Syncer(),

			log:            loghelper.New(syncer.Name()),
			vEventRecorder: &testingutil.FakeEventRecorder{},
			physicalClient: pClient,

			currentNamespace:       fakeContext.CurrentNamespace,
			currentNamespaceClient: fakeContext.CurrentNamespaceClient,

			mappings: fakeContext.Mappings,

			virtualClient: vClient,
			options:       options,

			locker: locker.New(),
		}

		// execute
		for _, req := range tc.EnqueueObjs {
			_, err = controller.Reconcile(ctx, ctrl.Request{NamespacedName: req})
		}
		if tc.shouldErr {
			assert.ErrorContains(t, err, tc.errMsg)
		} else {
			assert.NilError(t, err)
		}

		// assert expected result
		// Compare states
		if tc.ExpectedPhysicalState != nil {
			for gvk, objs := range tc.ExpectedPhysicalState {
				err := syncertesting.CompareObjs(ctx, t, tc.Name+" physical state", fakeContext.PhysicalManager.GetClient(), gvk, scheme.Scheme, objs, tc.Compare)
				if err != nil {
					t.Fatalf("%s - Physical State mismatch: %v", tc.Name, err)
				}
			}
		}
		if tc.ExpectedVirtualState != nil {
			for gvk, objs := range tc.ExpectedVirtualState {
				err := syncertesting.CompareObjs(ctx, t, tc.Name+" virtual state", fakeContext.VirtualManager.GetClient(), gvk, scheme.Scheme, objs, tc.Compare)
				if err != nil {
					t.Fatalf("%s - Virtual State mismatch: %v", tc.Name, err)
				}
			}
		}
	}
}
