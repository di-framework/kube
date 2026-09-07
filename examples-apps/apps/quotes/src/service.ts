import { Component, Container } from "@di-framework/core/decorators";
import { ProductCatalog } from "../../../shared/catalog";

export class InvalidQuote extends Error {}

@Container()
export class QuoteService {
  @Component(ProductCatalog)
  private catalog!: ProductCatalog;

  calculate(input: unknown) {
    if (!input || typeof input !== "object" || !("items" in input) || !Array.isArray(input.items)) {
      throw new InvalidQuote("Expected an object with an items array");
    }
    if (input.items.length < 1 || input.items.length > 50) {
      throw new InvalidQuote("Provide between 1 and 50 line items");
    }
    const items = input.items.map((item: unknown) => {
      if (!item || typeof item !== "object" || !("sku" in item) || typeof item.sku !== "string" ||
          !("quantity" in item) || typeof item.quantity !== "number" ||
          !Number.isInteger(item.quantity) || item.quantity < 1 || item.quantity > 100) {
        throw new InvalidQuote("Each item needs a sku and an integer quantity between 1 and 100");
      }
      const product = this.catalog.find(item.sku);
      if (!product) throw new InvalidQuote(`Unknown product: ${item.sku}`);
      return { ...product, quantity: item.quantity, totalCents: product.priceCents * item.quantity };
    });
    return {
      currency: "USD",
      items,
      totalCents: items.reduce((total, item) => total + item.totalCents, 0),
    };
  }
}
