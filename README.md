# golang-microservices-ecommerce

Go ile yazılmış e-ticaret mikroservis projesi. Alan dört servise bölünür; her servis kendi
verisine sahiptir ve problemine uygun kalıcılık teknolojisini kullanır.

Bu repo, [dotnet-microservices-ecommerce](https://github.com/mustafaakcakaya/dotnet-microservices-ecommerce)
projesinin **aynı mimariyle** Go'ya taşınmış halidir. Amaç yeni bir sistem tasarlamak değil,
var olan mimariyi Go'ya aktarmak ve iki repoyu karşılaştırılabilir tutmaktır.

## Durum

Port devam ediyor. Servisler .NET reposundaki sırayla eklenir: Catalog → Basket → Discount →
Ordering → outbox/CDC.

| Servis | Sorumluluk | Kalıcılık | Durum |
| --- | --- | --- | --- |
| Catalog | Ürün kataloğu | PostgreSQL | planlandı |
| Basket | Alışveriş sepeti | PostgreSQL + Redis | planlandı |
| Discount | İndirim kuponları | SQLite (gRPC) | planlandı |
| Ordering | Sipariş yaşam döngüsü | SQL Server | planlandı |
| ordering-worker | Outbox mesajlarını yayınlar | — | planlandı |

## Mimari yaklaşımlar

- **Clean Architecture** (Ordering): bağımlılıklar dışarıdan içeriye. `internal/ordering/domain`
  hiçbir altyapı paketini import etmez — bu kural linter ile zorlanır.
- **Domain-Driven Design**: `Order` aggregate root, value object'ler, domain event'ler.
- **CQRS**: komut ve sorgu ayrı handler'lar; kesişen ilgiler middleware zinciriyle.
- **Transactional Outbox + CDC**: aggregate değişikliği ile yayınlanacak mesaj aynı SQL
  transaction'ında yazılır; ayrı bir worker SQL Server CDC üzerinden okuyup broker'a yayınlar.
- **Domain event ↔ integration event ayrımı**: domain event içeride kalır, dışarıya yalnızca
  versiyonlanmış integration event çıkar. Hassas ödeme verisi (kart numarası, CVV) asla
  outbox'a, loglara veya broker'a girmez.
- **at-least-once teslimat + consumer inbox idempotency**: broker message id üzerinden
  tekrarlar elenir.

## Dizin yapısı

```
cmd/           her servis için bir main paketi
internal/
  platform/    servisler arası ortak kod (cqrs, errors, pagination, messaging, inbox)
  catalog/  basket/  discount/
  ordering/
    domain/    aggregate, value object, domain event — dış bağımlılık yok
    app/       command/query handler, dto, integration event mapper
    infra/     kalıcılık, migration, outbox, cdc reader, checkpoint store
deploy/        compose ve dağıtım tanımları
```

Tek `go.mod` kullanılır: servisler birlikte geliştirilip deploy edilir, çoklu modülün
sürüm pinleme yükü bu aşamada karşılığını vermez.

## Geliştirme

```bash
make build     # tüm paketleri derle
make test      # testleri çalıştır
make lint      # golangci-lint
make tidy      # go.mod düzenle
```

Go 1.24+ gerekir.
