FROM node:22-slim AS builder
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
# No VITE_API_BASE_URL is passed here on purpose: Vite inlines import.meta.env
# values at this build step, not at container start, so a docker-compose
# `environment:` entry on the web service would have no effect on the built
# bundle anyway. Leaving it unset keeps the default same-origin behavior
# (see src/lib/api.ts), which nginx.conf below makes work in production the
# same way vite.config.ts's dev proxy does locally.
RUN npm run build

FROM nginx:1.29-alpine
COPY --from=builder /src/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
