package skulimiter

import (
	"os"
	"testing"
	"time"

	desc "git.ozon.dev/dlukashov/sku-limiter/pkg/sku-limiter"
	"github.com/alicebob/miniredis"
	"github.com/go-redis/redis/v7"
	"github.com/stretchr/testify/assert"
)

// глобальная имплементация для тестов
var (
	impl *Implementation
)

func TestMain(m *testing.M) {
	os.Exit(testMain(m))
}

func testMain(m *testing.M) int {

	// подготавливаем мок с redis
	mr, err := miniredis.Run()
	if err != nil {
		panic(err)
	}
	client := redis.NewClient(&redis.Options{
		Addr:         mr.Addr(),
		DB:           0,
		DialTimeout:  10 * time.Second,
		ReadTimeout:  20 * time.Second,
		WriteTimeout: 20 * time.Second,
		PoolTimeout:  20 * time.Second,
	})

	_, err = client.Ping().Result()
	if err != nil {
		panic(err)

	}

	defer mr.Close()
	defer client.Close()

	impl = NewSKULimiter()
	impl.SetDB(client)

	return m.Run()
}

func TestFunctional(t *testing.T) {

	// тесты по операциям с sku
	t.Run("set sku limits", TestcreateLimitSKURs(impl))
	t.Run("get sku limits", TestgetLimitSKUnitsRs(impl))
	t.Run("del sku limits", TestdelLimitSKURs(impl))

	// тесты по операциям с заказами юзеров
	t.Run("add order user", TestaddOrderRs(impl))
	t.Run("return order user", TestreturnOrderRs(impl))
	t.Run("get limit user; 1 user, filter - skus", TestgetUserLimitsSKUnitsRs(impl))
	t.Run("get limit users; filter - users, actions", TestgetLimitsUsersActionsRs(impl))
	t.Run("delete (reset) limit user", TestdelLimitUserRs(impl))

}

func TestcreateLimitSKURs(i *Implementation) func(t *testing.T) {
	return func(t *testing.T) {

		var (
			actions map[string]*desc.CLRequest_Action
			skus    = make(map[string]*desc.CLRequest_Actions)
		)

		// заполним первичными данными для тестов
		actions = make(map[string]*desc.CLRequest_Action)
		actions[zeroAction] = &desc.CLRequest_Action{Limit: 15, Sec: 3600}
		actions["4"] = &desc.CLRequest_Action{Limit: 3, Sec: 90}
		skus["sss"] = &desc.CLRequest_Actions{Actions: actions}

		actions = make(map[string]*desc.CLRequest_Action)
		actions[zeroAction] = &desc.CLRequest_Action{Limit: 7, Sec: 36000}
		actions["2"] = &desc.CLRequest_Action{Limit: 3, Sec: 900}
		skus["www"] = &desc.CLRequest_Actions{Actions: actions}

		actions = make(map[string]*desc.CLRequest_Action)
		actions[zeroAction] = &desc.CLRequest_Action{Limit: 12, Sec: 800}
		actions["3"] = &desc.CLRequest_Action{Limit: 2, Sec: 80}
		skus["qqq"] = &desc.CLRequest_Actions{Actions: actions}

		ctx := i.db.Context()
		err := i.createLimitSKURs(ctx, &skus)
		t.Logf("error: %v; data: %v", err, skus)

		assert.Nil(t, err)
	}
}

func TestgetLimitSKUnitsRs(i *Implementation) func(t *testing.T) {
	return func(t *testing.T) {
		assert := assert.New(t)

		ctx := i.db.Context()
		skus := []string{"sss", "fff", "www"}
		filterActions := []string{}
		res, err := i.getLimitSKUnitsRs(ctx, &skus, &filterActions)
		t.Logf("result: %v; error: %v", *res, err)

		testCases := []struct {
			name    string
			sku     string
			nameAct string
			act     Action
			exist   bool // есть ли такое sku-action в базе
		}{
			{"sss-0", "sss", "0", Action{15, 3600, 0}, true},
			{"sss-4", "sss", "4", Action{3, 90, 0}, true},
			{"www-2", "www", "2", Action{3, 900, 0}, true},
			{"fff-nil", "fff", "0", Action{}, false},
		}

		for _, tc := range testCases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				assert.Contains(*res, tc.sku)
				if tc.exist {
					assert.Equal((*res)[tc.sku].Actions[tc.nameAct].Limit, tc.act.Limit)
					assert.Equal((*res)[tc.sku].Actions[tc.nameAct].Sec, tc.act.Sec)
				} else {
					assert.NotContains((*res)[tc.sku].Actions, tc.nameAct)
				}

			})
		}

		assert.Nil(err)

	}
}

func TestdelLimitSKURs(i *Implementation) func(t *testing.T) {
	return func(t *testing.T) {
		assert := assert.New(t)

		ctx := i.db.Context()
		skusDel := []string{"www", "fff"}
		filterActions := []string{zeroAction, "2", "4"}
		err := i.delLimitSKURs(ctx, &skusDel, &filterActions)
		t.Logf("error: %v", err)

		testCases := []struct {
			name    string
			sku     string
			nameAct string
			exist   bool // есть ли такое sku-action в базе (после удаления)
		}{
			{"www-0", "www", zeroAction, false},
			{"www-2", "www", "2", false},
			{"fff-nil", "fff", zeroAction, false},
			{"sss-0", "sss", zeroAction, true},
			// {"qqq-3", "qqq", "3", true},
		}

		for _, tc := range testCases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				_, err := i.db.HGet(prefix+tc.sku, tc.nameAct).Result()
				if tc.exist {
					assert.NoError(err)
				} else {
					// sku нет в базе (значит успешное удаление)
					assert.Error(err)
				}

			})
		}

	}
}

func TestaddOrderRs(i *Implementation) func(t *testing.T) {
	return func(t *testing.T) {

		var (
			ctx = i.db.Context()
		)

		testCases := []struct {
			name      string
			userIDInt int32
			OrderID   int32
			do        int64
			content   []*desc.AORequest_SKU
		}{
			{"user-100", 100, 1, time.Now().Unix(), []*desc.AORequest_SKU{
				&desc.AORequest_SKU{Sku: "sss", MarketingActionId: "4", Qty: 3},
				&desc.AORequest_SKU{Sku: "www", MarketingActionId: zeroAction, Qty: 2}},
			},
			{"user-200", 200, 5, time.Now().Unix(), []*desc.AORequest_SKU{
				&desc.AORequest_SKU{Sku: "sss", MarketingActionId: "4", Qty: 1}},
			},
			{"user-300", 300, 10, time.Now().Unix(), []*desc.AORequest_SKU{
				&desc.AORequest_SKU{Sku: "qqq", MarketingActionId: "3", Qty: 1}},
			},
		}

		for _, tc := range testCases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				err := i.addOrderRs(ctx, tc.userIDInt, tc.OrderID, tc.do, &tc.content)
				t.Logf("error: %v; userID: %v; date_order: %v", err, tc.userIDInt, tc.do)
				assert.Nil(t, err)

			})
		}
	}
}

func TestreturnOrderRs(i *Implementation) func(t *testing.T) {
	return func(t *testing.T) {

		var (
			ctx = i.db.Context()
		)

		testCases := []struct {
			name      string
			userIDInt int32
			OrderID   int32
			content   []*desc.RORequest_SKU
		}{
			{"user-100", 100, 1, []*desc.RORequest_SKU{
				&desc.RORequest_SKU{Sku: "www", Qty: 1}},
			},
			{"user-200", 200, 5, []*desc.RORequest_SKU{
				&desc.RORequest_SKU{Sku: "sss", Qty: 1}},
			},
		}

		for _, tc := range testCases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				err := i.returnOrderRs(ctx, tc.userIDInt, tc.OrderID, &tc.content)
				t.Logf("error: %v; userID: %v", err, tc.userIDInt)
				assert.Nil(t, err)

			})
		}
	}
}

func TestgetUserLimitsSKUnitsRs(i *Implementation) func(t *testing.T) {
	return func(t *testing.T) {

		assert := assert.New(t)
		ctx := i.db.Context()

		testCases := []struct {
			name   string
			userID string
			skus   []string
			expAns map[string]map[string]int32
		}{
			{"user-100", "100", []string{"sss"}, map[string]map[string]int32{"sss": map[string]int32{zeroAction: 12}}},
			{"user-200", "200", []string{"www", "qqq"}, map[string]map[string]int32{"www": map[string]int32{zeroAction: -1}, "qqq": map[string]int32{zeroAction: 12, "3": 2}}},
		}

		for _, tc := range testCases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				res, err := i.getUserLimitsSKUnitsRs(ctx, &tc.skus, tc.userID)
				t.Logf("error: %v; userID: %v; skus: %v", err, tc.userID, tc.skus)
				assert.Nil(err)
				for sku, limits := range *res {
					for act, val := range (*limits).Limit {
						assert.Equal(val, tc.expAns[sku][act])
					}
				}

			})
		}
	}
}

func TestgetLimitsUsersActionsRs(i *Implementation) func(t *testing.T) {
	return func(t *testing.T) {

		assert := assert.New(t)
		ctx := i.db.Context()

		testCases := []struct {
			name    string
			users   []string
			actions []string
			expAns  map[string]map[string]int32
		}{
			{"user-100", []string{"100", "200"}, []string{"0", "5"}, map[string]map[string]int32{"sss": map[string]int32{zeroAction: 12}}},
			{"user-100", []string{"100"}, []string{}, map[string]map[string]int32{"sss": map[string]int32{zeroAction: 12}}},
		}

		for _, tc := range testCases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				_, err := i.getLimitsUsersActionsRs(ctx, &tc.users, &tc.actions)
				assert.Nil(err)

			})
		}
	}
}

func TestdelLimitUserRs(i *Implementation) func(t *testing.T) {
	return func(t *testing.T) {
		assert := assert.New(t)

		ctx := i.db.Context()
		usersDel := []string{"100", "200"}
		filterActions := []string{}
		err := i.delLimitUserRs(ctx, &usersDel, &filterActions)
		t.Logf("error: %v", err)

		testCases := []struct {
			name   string
			user   string
			action string
			exist  bool // есть ли такой user-action в базе (после удаления)
		}{
			{"100-0", "100", zeroAction, false},
			{"100-2", "100", "2", false},
			{"300-3", "300", "3", true},
		}

		for _, tc := range testCases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				r, err := i.db.HGetAll(tc.user).Result()
				assert.NoError(err)
				if tc.exist {
					assert.Equal(len(r), 1)
				} else {
					// пустая мапа с покупками (успешное удаление)
					assert.Equal(len(r), 0)
				}

			})
		}

		assert.Nil(err)

	}
}

func BenchmarkCreateLimitSKURs(b *testing.B) {
	var (
		actions = make(map[string]*desc.CLRequest_Action, 20)
		skus    = make(map[string]*desc.CLRequest_Actions)
	)

	// заполним первичными данными для тестов
	actions = make(map[string]*desc.CLRequest_Action)
	actions[zeroAction] = &desc.CLRequest_Action{Limit: 15, Sec: 3600}
	actions["4"] = &desc.CLRequest_Action{Limit: 3, Sec: 90}
	skus["sss"] = &desc.CLRequest_Actions{Actions: actions}

	actions = make(map[string]*desc.CLRequest_Action)
	actions[zeroAction] = &desc.CLRequest_Action{Limit: 7, Sec: 36000}
	actions["2"] = &desc.CLRequest_Action{Limit: 3, Sec: 900}
	skus["www"] = &desc.CLRequest_Actions{Actions: actions}

	actions = make(map[string]*desc.CLRequest_Action)
	actions[zeroAction] = &desc.CLRequest_Action{Limit: 12, Sec: 800}
	actions["3"] = &desc.CLRequest_Action{Limit: 2, Sec: 80}
	skus["qqq"] = &desc.CLRequest_Actions{Actions: actions}

	newSKU := []string{"zzz", "xxx", "ccc", "vvv", "bbb", "nnn", "mmm", "lll", "kkk", "jjj"}
	for _, sku := range newSKU {
		actions = make(map[string]*desc.CLRequest_Action)
		actions[zeroAction] = &desc.CLRequest_Action{Limit: 5, Sec: 500}
		skus[sku] = &desc.CLRequest_Actions{Actions: actions}
	}

	ctx := impl.db.Context()

	b.ReportAllocs()
	b.ResetTimer()
	impl.createLimitSKURs(ctx, &skus)
}

func BenchmarkGetLimitSKUnitsRs(b *testing.B) {

	ctx := impl.db.Context()
	skus := []string{"sss", "www", "qqq", "zzz", "xxx", "ccc", "vvv", "bbb", "nnn", "mmm", "lll", "kkk", "jjj"}
	filterActions := []string{}

	b.ReportAllocs()
	b.ResetTimer()
	impl.getLimitSKUnitsRs(ctx, &skus, &filterActions)

}

func BenchmarkAddOrderRs(b *testing.B) {

	var ctx = impl.db.Context()

	type testCases struct {
		userIDInt int32
		OrderID   int32
		do        int64
		content   []*desc.AORequest_SKU
	}
	tc := testCases{100, 1, time.Now().Unix(), []*desc.AORequest_SKU{
		&desc.AORequest_SKU{Sku: "sss", MarketingActionId: "4", Qty: 3},
		&desc.AORequest_SKU{Sku: "www", MarketingActionId: zeroAction, Qty: 2},
		&desc.AORequest_SKU{Sku: "qqq", MarketingActionId: "3", Qty: 1}},
	}

	b.ReportAllocs()
	b.ResetTimer()
	impl.addOrderRs(ctx, tc.userIDInt, tc.OrderID, tc.do, &tc.content)

}

func BenchmarkGetUserLimitsSKUnitsRs(b *testing.B) {

	ctx := impl.db.Context()
	skus := []string{"sss", "www", "qqq", "zzz", "xxx", "ccc", "vvv", "bbb", "nnn", "mmm", "lll", "kkk", "jjj"}
	user := "100"

	b.ReportAllocs()
	b.ResetTimer()
	impl.getUserLimitsSKUnitsRs(ctx, &skus, user)

}

func BenchmarkGetLimitsUsersActionsRs(b *testing.B) {

	ctx := impl.db.Context()
	users := []string{"100", "200"}
	actions := []string{zeroAction, "2", "3", "4"}

	b.ReportAllocs()
	b.ResetTimer()
	impl.getLimitsUsersActionsRs(ctx, &users, &actions)

}

func BenchmarkDelLimitUserRs(b *testing.B) {

	ctx := impl.db.Context()
	users := []string{"100", "200"}
	actions := []string{zeroAction, "2", "3", "4"}

	b.ReportAllocs()
	b.ResetTimer()
	impl.delLimitUserRs(ctx, &users, &actions)

}

func BenchmarkReturnOrderRs(b *testing.B) {

	var ctx = impl.db.Context()

	type testCases struct {
		userIDInt int32
		OrderID   int32
		do        int64
		content   []*desc.RORequest_SKU
	}
	tc := testCases{100, 1, time.Now().Unix(), []*desc.RORequest_SKU{
		&desc.RORequest_SKU{Sku: "sss", Qty: 1},
		&desc.RORequest_SKU{Sku: "www", Qty: 1},
		&desc.RORequest_SKU{Sku: "qqq", Qty: 1}},
	}

	b.ReportAllocs()
	b.ResetTimer()
	impl.returnOrderRs(ctx, tc.userIDInt, tc.OrderID, &tc.content)

}
