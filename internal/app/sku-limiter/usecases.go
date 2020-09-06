package skulimiter

import (
	"context"
	"strconv"
	"sync"
	"time"

	desc "git.ozon.dev/dlukashov/sku-limiter/pkg/sku-limiter"
	"github.com/vmihailenco/msgpack/v4"
	"gitlab.ozon.ru/platform/tracer-go/logger"
	"go.uber.org/zap"
)

const (
	zeroAction = "0"
	prefix     = "-" // для избежания ситуаций совпадения ключей sku и user_id
)

// Action структура для акций
type Action struct {
	Limit     int32
	Sec       int64
	Datestart int64
}

// Buy структура для покупок пользователей
type Buy struct {
	Action   string
	Qty      int32
	DateKill int64
	OrderID  int32
}

// CreateLimitSKURs сохранить SKU и его акции
func (i *Implementation) createLimitSKURs(ctx context.Context, skus map[string]*desc.CLRequest_Actions) error {

	ds := time.Now().Unix()
	wg := &sync.WaitGroup{}
	wg.Add(len(skus))
	for sku, el := range skus {
		go func(sku string, el *desc.CLRequest_Actions) {
			defer wg.Done()
			for action, val := range el.Actions {

				b, err := msgpack.Marshal(&Action{Limit: val.Limit, Sec: val.Sec, Datestart: ds})
				if err != nil {
					logger.Error(ctx, "Marshal sku action error", zap.String("sku", sku), zap.String("action", action), err)
					continue
				}

				err = i.db.HSet(prefix+sku, action, b).Err()
				if err != nil {
					logger.Error(ctx, "Failed save to redis", zap.String("sku", sku), zap.String("action", action), err)
					continue
				}

			}
		}(sku, el)
	}
	wg.Wait()
	return nil
}

// getLimitSKUnitsRs вернуть список акций для списка SKU
func (i *Implementation) getLimitSKUnitsRs(ctx context.Context, skus []string, filterActions []string) (*map[string]*desc.GLResponse_Actions, error) {

	actionsAllSKU := make(map[string]*desc.GLResponse_Actions)

	wg := &sync.WaitGroup{}
	mu := &sync.Mutex{}
	wg.Add(len(skus))
	for _, sku := range skus {

		go func(sku string) {
			defer wg.Done()
			actionsSKU, err := i.getSKULimits(ctx, sku, &filterActions)
			if err != nil {
				logger.Error(ctx, "Failed get sku actions", zap.String("sku", sku), err)
				return
				// return &actionsAllSKU, err
			}

			// тут мапа для товаров и их акций
			mu.Lock()
			actionsAllSKU[sku] = &desc.GLResponse_Actions{Actions: *actionsSKU}
			mu.Unlock()

		}(sku)
	}

	wg.Wait()
	return &actionsAllSKU, nil

}

// getSKULimits вернуть список акций для одного SKU
func (i *Implementation) getSKULimits(ctx context.Context, sku string, filterActions *[]string) (*map[string]*desc.GLResponse_Action, error) {

	res := make(map[string]*desc.GLResponse_Action) // акции одного товара

	obj, err := i.db.HGetAll(prefix + sku).Result()
	if err != nil {
		logger.Error(ctx, "Failed HGetAll redis", zap.String("sku", sku), err)
		return &res, err
	}

	existAct := true
	for key, el := range obj {

		// есть ли список с акциями (если пустой, то смотрим все акции)
		if len(*filterActions) > 0 {
			existAct = contains(filterActions, key)
		}

		if !existAct {
			continue
		}

		act := desc.GLResponse_Action{}
		err = msgpack.Unmarshal([]byte(el), &act)
		if err != nil {
			logger.Error(ctx, "Failed Unmarshal redis", zap.String("sku", sku), zap.String("action", key), err)
			continue
		}

		res[key] = &act
	}

	return &res, nil
}

// delLimitSKURs удалить все лимиты SKU в списке (или определённые акции)
func (i *Implementation) delLimitSKURs(ctx context.Context, listDelSKU []string, listDelAction []string) error {

	if len(listDelAction) > 0 {
		for _, sku := range listDelSKU {
			err := i.db.HDel(prefix+sku, listDelAction...).Err()
			if err != nil {
				logger.Error(ctx, "Failed delete sku actions", zap.String("sku", sku), zap.Strings("actions", listDelAction), err)
				continue
			}
		}

	} else {
		// добавить всем префикс чтобы можно было найти ключ в redis
		ind := 0
		for ind < len(listDelSKU) {
			listDelSKU[ind] = prefix + listDelSKU[ind]
			ind++
		}
		err := i.db.Del(listDelSKU...).Err()

		if err != nil {
			logger.Error(ctx, "Failed delete sku", zap.Strings("skus", listDelSKU), err)
		}

	}

	return nil
}

// вернуть 1 акцию sku
func (i *Implementation) getSKULimit(ctx context.Context, sku string, action string) (Action, error) {

	act := Action{}

	obj, err := i.db.HGet(prefix+sku, action).Bytes()
	// нет настройки для для такого сочетания sku-action
	if err != nil {
		return act, err
	}

	err = msgpack.Unmarshal(obj, &act)
	if err != nil {
		return act, err
	}

	return act, nil

}

// addOrderRs сохранить заказ с TTL = "окно" + order_ts (дата покупки)
func (i *Implementation) addOrderRs(ctx context.Context, userIDInt int32, OrderID int32, do int64, content []*desc.AORequest_SKU) error {

	newMaxTTL := false
	userID := strconv.FormatInt(int64(userIDInt), 10)
	maxTTL := i.getMaxTTLUser(ctx, userID)
	// мапа для обработанных sku
	processedSKUs := make(map[string]bool, len(content))

	wg := &sync.WaitGroup{}
	wg.Add(len(content))
	mu := &sync.Mutex{}

	for _, c := range content {
		go func(c *desc.AORequest_SKU) {
			defer wg.Done()

			buys, _ := i.getSKUUserFromRedis(ctx, userID, c.Sku)

			// рассчитываем время когда товар выйдет за окно
			action, err := i.getSKULimit(ctx, c.Sku, c.MarketingActionId)

			if err != nil {
				logger.Error(ctx, "Failed to add order", zap.Int32("OrderID", OrderID), zap.String("userID", userID), zap.String("sku", c.Sku), err)
				return
			}

			// если акция не описана в настройках, то сюда не попадём
			if action.Sec > 0 {
				dk := do + action.Sec

				// вытаскиваем текущие покупки (без вышедших за окно) по этому товару и добавляем новый
				newBuy := Buy{Action: c.MarketingActionId, Qty: c.Qty, DateKill: dk, OrderID: OrderID}
				*buys = append(*buys, newBuy)

				if maxTTL < dk {
					maxTTL = dk
					newMaxTTL = true
				}
			}

			// по нулевой акции всегда добавляем покупку (нулевой акции может и не быть)
			if c.MarketingActionId != zeroAction {
				action, err := i.getSKULimit(ctx, c.Sku, zeroAction)
				if err != nil {
					logger.Debug(ctx, "Not found zero action", zap.Int32("OrderID", OrderID), zap.String("userID", userID), zap.String("sku", c.Sku), err)
					return
				}
				if action.Sec > 0 {
					dk0 := do + action.Sec
					newBuyZero := Buy{Action: zeroAction, Qty: c.Qty, DateKill: dk0, OrderID: OrderID}
					*buys = append(*buys, newBuyZero)
					if maxTTL < dk0 {
						maxTTL = dk0
						newMaxTTL = true
					}
				}
			}

			err = i.updateRecBuy(ctx, buys, userID, c.Sku)
			if err != nil {
				logger.Error(ctx, "Failed to add buy", zap.Int32("OrderID", OrderID), zap.String("userID", userID), zap.String("sku", c.Sku), err)
				return
			}

			// тут мапа для товаров и их акций
			mu.Lock()
			processedSKUs[c.Sku] = true
			mu.Unlock()

		}(c)
	}

	wg.Wait()

	// NOTE: смысл этого кода в очистке "мусорных" записей
	// Если юзер долго ничего не берёт и ручку с ним не дёргают, то записи зависнут
	// этот код решает эту проблему (т.е. килл ключа юзера когда все его покупки вышли за окно)
	if newMaxTTL {
		i.db.ExpireAt(userID, time.Unix(maxTTL, 0))
	}

	// NOTE: актуализируем в фоне другие покупки пользователя (чтобы не было мёртвых записей)
	// актуально когда, например, user взял товар sku11, а потом начал постоянно брать другой товар - sku22 (TTL ключа user будет постоянно обновляться)
	// при таком сценарии запись покупки sku11, будет висеть когда окно уже выйдет, т.к. пользователь постоянно что-то берёт (но не sku11)
	go i.updateOtherSKUs(ctx, userID, &processedSKUs)

	return nil
}

// актуализировать покупки пользователя (удалить вышедшие за окно)
func (i *Implementation) updateOtherSKUs(ctx context.Context, userID string, processedSKUs *map[string]bool) {
	skus, err := i.db.HKeys(userID).Result()
	if err != nil {
		logger.Error(ctx, "Failed HKeys skus user redis", zap.String("userID", userID), err)
		return
	}

	for _, sku := range skus {
		ok := (*processedSKUs)[sku]
		if ok {
			continue
		}

		buys, editRec := i.getSKUUserFromRedis(ctx, userID, sku)

		if editRec {
			err = i.updateRecBuy(ctx, buys, userID, sku)
			if err != nil {
				logger.Error(ctx, "Failed to update other buys", zap.String("userID", userID), zap.String("sku", sku), err)
			}
		}

	}

}

// вернуть оставшееся время ключа (покупки юзера)
func (i *Implementation) getMaxTTLUser(ctx context.Context, user string) int64 {

	var maxTTL int64

	sec, err := i.db.TTL(user).Result()
	if err != nil {
		return maxTTL
	}

	maxTTL = time.Now().Unix() + int64(sec.Seconds())

	return maxTTL

}

// getSKUUserFromRedis получить заказы пользователя по конкретному sku
// editRec - если истина значит есть покупки вышедшие за окно, надо обновить запись
func (i *Implementation) getSKUUserFromRedis(ctx context.Context, userID string, sku string) (*[]Buy, bool) {

	actionList := make([]Buy, 0, 1)
	editRec := false

	buyByte, err := i.db.HGet(userID, sku).Bytes()
	// sku не описан в настройках или покупок по этому товару не было
	if err != nil {
		return &actionList, editRec
	}

	err = msgpack.Unmarshal(buyByte, &actionList)
	if err != nil {
		logger.Error(ctx, "Failed Unmarshal redis", zap.String("user", userID), zap.String("sku", sku), err)
	}

	// проверяем товары вывалившиеся за окно и удаляем их из слайса
	dt := time.Now().Unix()

	ind := 0
	for ind < len(actionList) {
		if actionList[ind].DateKill < dt {
			delElSlice(&actionList, ind)
			editRec = true
			continue
		}
		ind++
	}

	return &actionList, editRec

}

// обновляем запись по покупкам пользователя по товару
func (i *Implementation) updateRecBuy(ctx context.Context, buys *[]Buy, userID string, sku string) error {

	if len(*buys) == 0 {
		err := i.db.HDel(userID, sku).Err()
		return err
	}

	b, err := msgpack.Marshal(*buys)
	if err != nil {
		return err
	}

	err = i.db.HSet(userID, sku, b).Err()
	if err != nil {
		return err
	}

	return nil
}

// getUserLimitsSKUnitsRs вернуть список остатков sku по покупкам для одного юзера
func (i *Implementation) getUserLimitsSKUnitsRs(ctx context.Context, skus []string, userID string) (*map[string]*desc.GLUSResponse_Limit, error) {

	userLimitsSKUnits := make(map[string]*desc.GLUSResponse_Limit)

	wg := &sync.WaitGroup{}
	mu := &sync.Mutex{}
	wg.Add(len(skus))

	for _, sku := range skus {
		go func(sku string) {
			defer wg.Done()
			balance, err := i.getUserLimitsSKU(ctx, sku, userID)
			if err != nil {
				return
			}

			mu.Lock()
			userLimitsSKUnits[sku] = &desc.GLUSResponse_Limit{Limit: *balance}
			mu.Unlock()

		}(sku)
	}
	wg.Wait()
	return &userLimitsSKUnits, nil
}

// getUserLimitsSKU вернуть остатки по 1 sku по покупкам для одного юзера
func (i *Implementation) getUserLimitsSKU(ctx context.Context, sku string, userID string) (*map[string]int32, error) {

	var err error
	balance := make(map[string]int32)

	buys, editRec := i.getSKUUserFromRedis(ctx, userID, sku)

	if editRec {
		err = i.updateRecBuy(ctx, buys, userID, sku)
		if err != nil {
			logger.Error(ctx, "Failed to update buys", zap.String("userID", userID), zap.String("sku", sku), err)
			return &balance, err
		}
	}

	emptyList := []string{}
	actionsSKU, err := i.getSKULimits(ctx, sku, &emptyList)
	if err != nil {
		return &balance, err
	}

	if len(*actionsSKU) == 0 {
		balance[zeroAction] = -1
	} else {
		for action, el := range *actionsSKU {
			cnt := el.Limit
			for _, b := range *buys {
				if action == b.Action {
					cnt -= b.Qty
				}
			}
			balance[action] = cnt
		}
	}

	return &balance, nil
}

// getLimitsUsersActionsRs вернуть список остатков для списка юзеров + фильтр по акциям
func (i *Implementation) getLimitsUsersActionsRs(ctx context.Context, users []string, filterActions []string) (*map[string]*desc.GLUAResponse_SKUs, error) {

	usersActions := make(map[string]*desc.GLUAResponse_SKUs)

	wg := &sync.WaitGroup{}
	mu := &sync.Mutex{}
	wg.Add(len(users))

	for _, user := range users {
		go func(user string) {
			defer wg.Done()
			balanceSKUs, err := i.getUserLimitsActions(ctx, user, &filterActions)
			if err != nil {
				return
			}
			mu.Lock()
			usersActions[user] = &desc.GLUAResponse_SKUs{Skus: *balanceSKUs}
			mu.Unlock()
		}(user)
	}
	wg.Wait()
	return &usersActions, nil
}

// getUserLimitsActions вернуть список остатков для списка 1 юзера
func (i *Implementation) getUserLimitsActions(ctx context.Context, userID string, filterActions *[]string) (*map[string]*desc.GLUAResponse_Limit, error) {

	balanceSKUs := make(map[string]*desc.GLUAResponse_Limit)
	var err error

	// список sku у пользователя
	skuList, err := i.db.HKeys(userID).Result()

	if err != nil {
		logger.Error(ctx, "Failed HKeys redis", zap.String("userID", userID), err)
		return &balanceSKUs, err
	}

	for _, sku := range skuList {
		balance := make(map[string]int32)

		buys, editRec := i.getSKUUserFromRedis(ctx, userID, sku)
		if editRec {
			err = i.updateRecBuy(ctx, buys, userID, sku)
			if err != nil {
				logger.Error(ctx, "Failed to update buys", zap.String("userID", userID), zap.String("sku", sku), err)
				continue
			}
		}

		paramList := []string{}
		if len(*filterActions) > 0 {
			paramList = append(paramList, *filterActions...)
		}

		actionsSKU, err := i.getSKULimits(ctx, sku, &paramList)
		if err != nil {
			return &balanceSKUs, err
		}

		for action, el := range *actionsSKU {
			cnt := el.Limit
			for _, b := range *buys {
				if action == b.Action {
					cnt -= b.Qty
				}
			}
			balance[action] = cnt
		}

		balanceSKUs[sku] = &desc.GLUAResponse_Limit{Limit: balance}

	}

	return &balanceSKUs, nil
}

// returnOrderRs возврат товаров
func (i *Implementation) returnOrderRs(ctx context.Context, userIDInt int32, OrderID int32, content []*desc.RORequest_SKU) error {

	userID := strconv.FormatInt(int64(userIDInt), 10)

	wg := &sync.WaitGroup{}
	wg.Add(len(content))

	for _, c := range content {

		go func(c *desc.RORequest_SKU) {
			defer wg.Done()
			editRec := false
			buys, _ := i.getSKUUserFromRedis(ctx, userID, c.Sku)
			// найдём заказ по которому делается возврат, для выяснения акции
			// если заказ не найден, значит он вышел за окно и ничего не делаем
			// может быть больше одной записи (т.к. при наличии нулевой акции по ней тоже нужно вернуть)
			for ind, buy := range *buys {
				if buy.OrderID == OrderID {
					// вытаскиваем номер акции
					(*buys)[ind].Qty -= c.Qty
					editRec = true
				}
			}

			if editRec {
				err := i.updateRecBuy(ctx, buys, userID, c.Sku)
				if err != nil {
					logger.Error(ctx, "Failed to update buys", zap.String("userID", userID), zap.String("sku", c.Sku), err)
					return
				}
			}

		}(c)
	}

	wg.Wait()
	return nil
}

// delLimitUserRs удаляем пользователский покупки, если есть список акций, то только по акциям.
func (i *Implementation) delLimitUserRs(ctx context.Context, listDelUser []string, listDelAction []string) error {

	if len(listDelAction) > 0 {

		wg := &sync.WaitGroup{}
		wg.Add(len(listDelUser))

		for _, userID := range listDelUser {
			go func(userID string) {
				defer wg.Done()
				// список sku у пользователя
				skuList, err := i.db.HKeys(userID).Result()

				if err != nil {
					logger.Error(ctx, "Didn't find the keys redis", zap.String("userID", userID), err)
					return
				}

				for _, sku := range skuList {
					buys, editRec := i.getSKUUserFromRedis(ctx, userID, sku)

					for _, action := range listDelAction {

						ind := 0
						// пробегаемся по покупкам, если акция находится - удаляем
						for ind < len(*buys) {
							if (*buys)[ind].Action == action {
								delElSlice(buys, ind)
								editRec = true
								continue
							}
							ind++
						}

						if editRec {
							err := i.updateRecBuy(ctx, buys, userID, sku)
							if err != nil {
								logger.Error(ctx, "Failed to update buys", zap.String("userID", userID), zap.String("sku", sku), err)
								continue
							}
						}
					}
				}
			}(userID)
		}

		wg.Wait()

	} else {
		err := i.db.Del(listDelUser...).Err()
		if err != nil {
			logger.Error(ctx, "Error removing from redis", zap.Strings("listDelUser", listDelUser), err)
		}
	}

	return nil
}

// проверка вхождения значения в слайс
func contains(paramList *[]string, action string) bool {

	for _, n := range *paramList {
		if action == n {
			return true
		}
	}
	return false
}

// удалить элемент слайса
func delElSlice(mas *[]Buy, i int) {

	copy((*mas)[i:], (*mas)[i+1:])
	(*mas)[len(*mas)-1] = Buy{}
	(*mas) = (*mas)[:len((*mas))-1]

}
