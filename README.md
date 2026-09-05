# golang-microservices-ecommerce

Go ile yazılmış e-ticaret mikroservis projesi. Alan dört servise bölünür; her servis kendi
verisine sahiptir ve problemine uygun kalıcılık teknolojisini kullanır.

## Servisler

| Servis | Sorumluluk | Kalıcılık | Durum |
| --- | --- | --- | --- |
| Catalog | Ürün kataloğu | PostgreSQL | hazır |
| Basket | Alışveriş sepeti | PostgreSQL + Redis | hazır |
| Discount | İndirim kuponları | SQLite (gRPC) | hazır |
| Ordering | Sipariş yaşam döngüsü | SQL Server | planlandı |
| ordering-worker | Outbox mesajlarını yayınlar | — | planlandı |

## Mimari yaklaşımlar

- **Clean Architecture** (Ordering): bağımlılıklar dışarıdan içeriye. `internal/ordering/domain`
  hiçbir altyapı paketini import etmez — bu kural linter ile zorlanır.
- **Domain-Driven Design**: `Order` aggregate root, value object'ler, domain event'ler.
- **CQRS**: komut ve sorgular ayrı handler'lar; kesişen ilgiler middleware zinciriyle eklenir.
  Dispatcher yoktur, handler'lar `main` içinde açıkça bağlanır.
- **Vertical slice**: her özellik kendi dosyasında isteği, handler'ı ve HTTP route'uyla birlikte
  durur; bir davranış tek yerden okunur ve değiştirilir.
- **Transactional Outbox + CDC**: aggregate değişikliği ile yayınlanacak mesaj aynı SQL
  transaction'ında yazılır; ayrı bir worker SQL Server CDC üzerinden okuyup broker'a yayınlar.
- **Domain event ↔ integration event ayrımı**: domain event içeride kalır, dışarıya yalnızca
  versiyonlanmış integration event çıkar. Hassas ödeme verisi (kart numarası, CVV) outbox'a,
  loglara veya broker'a hiç girmez.
- **at-least-once teslimat + consumer inbox idempotency**: broker message id üzerinden
  tekrarlar elenir.

## Dizin yapısı

```
cmd/           her servis için bir main paketi
internal/
  platform/    servisler arası ortak kod (cqrs, apperr, pagination, validation, httpx, health)
  catalog/  basket/  discount/
  ordering/
    domain/    aggregate, value object, domain event — dış bağımlılık yok
    app/       command/query handler, dto, integration event mapper
    infra/     kalıcılık, migration, outbox, cdc reader, checkpoint store
proto/         servisler arası gRPC sözleşmeleri
deploy/        compose ve dağıtım tanımları
```

Tek `go.mod` kullanılır: servisler birlikte geliştirilip deploy edilir, çoklu modülün sürüm
pinleme yükü bu aşamada karşılığını vermez.

## Çalıştırma

```bash
docker compose -f deploy/compose.yaml up --build
```

Servis adresleri: Catalog `6100`, Basket `6101`, Discount `6102` (gRPC). Portlar ortam
değişkenleriyle değiştirilebilir; ayrıntı `deploy/compose.yaml` başındaki tabloda.

Bağımlılıklar bilinçli olarak zorunludur: Basket, Redis veya Discount olmadan başlamaz. Eksik
bir cache sessizce yavaşlığa, eksik bir indirim servisi ise sessizce yanlış fiyata yol açardı —
ikisi de deploy'dan çok sonra fark edilirdi.

## Geliştirme

```bash
make build     # tüm paketleri derle
make test      # testleri çalıştır (yarış tespiti açık)
make lint      # golangci-lint
make proto     # .proto dosyalarından Go kodunu yeniden üret (buf gerekir)
```

Go 1.26+ gerekir. Üretilmiş protobuf kodu depoya dahildir, dolayısıyla derlemek için protobuf
araç zincirine ihtiyaç yoktur.

Integration testleri Testcontainers ile gerçek PostgreSQL ve Redis ayağa kaldırır; Docker
gerektirmeyen hızlı paket için `go test -short ./...` kullanın.
