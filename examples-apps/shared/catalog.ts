import { Container } from "@di-framework/core/decorators";

export interface Product {
  sku: string;
  name: string;
  priceCents: number;
}

// Read-only fixtures: a Wasm instance's memory is not a durable database.
const products: readonly Product[] = [
  { sku: "mug", name: "DI Framework Mug", priceCents: 1500 },
  { sku: "shirt", name: "WebAssembly Shirt", priceCents: 2500 },
  { sku: "stickers", name: "Kubesolo Sticker Pack", priceCents: 500 },
];

@Container()
export class ProductCatalog {
  list(search = ""): Product[] {
    const query = search.trim().toLowerCase();
    return products
      .filter((product) => `${product.sku} ${product.name}`.toLowerCase().includes(query))
      .map((product) => ({ ...product }));
  }

  find(sku: string): Product | undefined {
    const product = products.find((product) => product.sku === sku);
    return product ? { ...product } : undefined;
  }
}
