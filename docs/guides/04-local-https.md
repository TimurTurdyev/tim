# Локальная разработка: от клона до https://shop.test

Зачем вообще https локально. На `localhost` браузер разрешает почти всё. А на нормальном
локальном домене вроде `shop.test` без настоящего сертификата не работают куки `Secure`
и `SameSite=None`, а с ними - вход через Google и другие OAuth и всё, что живёт в iframe.
Ошибка всплывает только на проде.

## Поднять склонированный проект

```sh
tim setup git@github.com:me/shop.git
```

tim склонирует репозиторий туда, где лежат остальные проекты, запишет его в реестр и доделает
то, чего не хватает: `.env` из `.env.example`, зависимости php, js, go или python, у Laravel -
ключ приложения, файл базы sqlite, миграции (спросит отдельно), ссылку на хранилище.
На готовом проекте `tim setup shop` ничего не делает - уже сделанное не повторяется.

Посмотреть план, ничего не меняя: `tim setup shop --dry-run`.

## Свой домен и сертификат

Один раз - доверить свой удостоверяющий центр системе и Firefox:

```sh
tim cert ca install
```

Потом для каждого проекта:

```sh
tim link shop                # shop.test в /etc/hosts, сертификат, url в реестре
tim serve shop --https       # https://shop.test:8443
tim serve shop --https --open  # и открыть в браузере, когда поднимется
```

Почему порт 8443, а не 443: на 443 без root не встать, а на macOS его часто держит OrbStack.

**Laravel.** Приложение должно доверять прокси, иначе оно увидит http и наделает ссылок с http.
В Laravel 11 и новее - `trustProxies(at: '*')` в `bootstrap/app.php`, в старых - `TrustProxies`.

Убрать всё обратно: `tim unlink shop`.

## Много поддоменов

`/etc/hosts` знает только точные имена. Если приложению нужны `a.shop.test`, `b.shop.test`
и так далее, проще отдать всю зону `.test` на свой компьютер:

```sh
brew install dnsmasq
tim host resolver test
```

## Имена без проектов

```sh
tim host add api.test admin.test --profile work
tim host off work      # выключить набор имён, не удаляя
tim host               # что сейчас в /etc/hosts от tim
```

tim ведёт в `/etc/hosts` свой блок и не трогает ничего за его пределами, а прежний файл
сохраняет рядом в `/etc/hosts.tim-backup`.

## Мелочи на каждый день

```sh
tim env diff shop      # каких ключей не хватает в .env по сравнению с .env.example
tim db shop            # клиент базы по .env проекта
tim logs shop -f       # свежий лог проекта
tim php use 8.3        # другая версия php в этом окне терминала
tim serve list         # что запущено в фоне; tim serve stop all - погасить
```
